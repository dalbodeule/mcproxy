package gate

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/robinbraemer/event"
	"go.minekube.com/common/minecraft/component"
	gatecfg "go.minekube.com/gate/pkg/edition/java/config"
	"go.minekube.com/gate/pkg/edition/java/ping"
	javaproxy "go.minekube.com/gate/pkg/edition/java/proxy"
	"go.minekube.com/gate/pkg/util/favicon"

	"mcproxy/internal/config"
	"mcproxy/internal/observability"
	"mcproxy/internal/store"
)

type Runtime struct {
	mu           sync.RWMutex
	proxy        *javaproxy.Proxy
	bind         string
	store        *store.Store
	tryOrder     []string
	routes       map[string]string
	syncEvery    time.Duration
	pingCacheTTL time.Duration
	pingFallback pingFallback
	pingCache    map[string]cachedPing
}

type pingFallback struct {
	motd        string
	offlineMotd string
	favicon     string
}

type cachedPing struct {
	payload   *ping.ServerPing
	expiresAt time.Time
}

type Options struct {
	Bind       string
	OnlineMode bool
	Store      *store.Store
	Config     config.Config
}

func New(ctx context.Context, opts Options) (*Runtime, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("store is required")
	}
	servers, err := opts.Store.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("no backend servers registered")
	}

	cfg := gatecfg.DefaultConfig
	cfg.Bind = opts.Bind
	cfg.OnlineMode = opts.OnlineMode
	forwardingMode := strings.ToLower(strings.TrimSpace(opts.Config.GateForwardingMode))
	if forwardingMode == "" {
		forwardingMode = string(gatecfg.LegacyForwardingMode)
	}
	cfg.Forwarding.Mode = gatecfg.ForwardingMode(forwardingMode)
	cfg.Forwarding.VelocitySecret = opts.Config.GateVelocitySecret
	cfg.Forwarding.BungeeGuardSecret = opts.Config.GateBungeeGuardSecret
	switch cfg.Forwarding.Mode {
	case gatecfg.NoneForwardingMode, gatecfg.LegacyForwardingMode:
	case gatecfg.VelocityForwardingMode:
		if strings.TrimSpace(cfg.Forwarding.VelocitySecret) == "" {
			return nil, fmt.Errorf("Gate velocity forwarding requires MCPROXY_GATE_VELOCITY_SECRET")
		}
	case gatecfg.BungeeGuardForwardingMode:
		if strings.TrimSpace(cfg.Forwarding.BungeeGuardSecret) == "" {
			return nil, fmt.Errorf("Gate bungeeguard forwarding requires MCPROXY_GATE_BUNGEGUARD_SECRET")
		}
	default:
		return nil, fmt.Errorf("unsupported Gate forwarding mode %q", forwardingMode)
	}
	cfg.Servers = map[string]string{}
	cfg.Try = make([]string, 0, len(servers))
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		cfg.Servers[s.Name] = s.Upstream
		cfg.Try = append(cfg.Try, s.Name)
	}
	if len(cfg.Try) == 0 {
		return nil, fmt.Errorf("no enabled backend servers registered")
	}

	eventMgr := event.New()
	p, err := javaproxy.New(javaproxy.Options{Config: &cfg, EventMgr: eventMgr})
	if err != nil {
		return nil, err
	}

	r := &Runtime{
		proxy:        p,
		bind:         opts.Bind,
		store:        opts.Store,
		syncEvery:    5 * time.Second,
		tryOrder:     append([]string(nil), cfg.Try...),
		routes:       make(map[string]string),
		pingCacheTTL: time.Duration(opts.Config.PingCacheTTL) * time.Second,
		pingFallback: pingFallback{motd: opts.Config.PingFallbackMotd, offlineMotd: opts.Config.PingOfflineMotd, favicon: opts.Config.PingFallbackFavicon},
		pingCache:    make(map[string]cachedPing),
	}
	event.Subscribe(p.Event(), 0, func(e *javaproxy.PreLoginEvent) {
		remoteIP := netIPString(e.Conn().RemoteAddr())
		res, err := opts.Store.EvaluateAttempt(context.Background(), store.EvaluateAttemptInput{
			IP:       remoteIP,
			Nickname: e.Username(),
			Record:   boolPtr(true),
		})
		if err != nil {
			observability.Error("gate_prelogin_evaluate_failed", "error", err, "remote_ip", remoteIP, "username", e.Username())
			e.Deny(&component.Text{Content: "Security policy evaluation failed"})
			return
		}
		if !res.Allowed {
			observability.Warn("gate_prelogin_denied", "remote_ip", remoteIP, "username", e.Username(), "reason", res.Reason)
			e.Deny(&component.Text{Content: "Connection denied: " + res.Reason})
		}
	})

	event.Subscribe(p.Event(), 0, func(e *javaproxy.PingEvent) {
		host := hostnameFromAddr(e.Connection().VirtualHost())
		observability.Info("gate_ping", "remote_ip", netIPString(e.Connection().RemoteAddr()), "host", host)
		e.SetPing(r.resolvePing(host))
	})

	event.Subscribe(p.Event(), 0, func(e *javaproxy.PlayerChooseInitialServerEvent) {
		host := hostnameFromAddr(e.Player().VirtualHost())
		if initial := r.pickInitialServer(host); initial != nil {
			observability.Info("gate_initial_server_selected", "host", host, "selected_server", initial.ServerInfo().Name(), "selected_upstream", initial.ServerInfo().Addr().String())
			e.SetInitialServer(initial)
		} else {
			observability.Warn("gate_initial_server_missing", "host", host)
		}
	})
	event.Subscribe(p.Event(), 0, func(e *javaproxy.ServerPostConnectEvent) {
		player := e.Player()
		if player.CurrentServer() == nil {
			observability.Warn("gate_player_connected_without_current_server", "username", player.Username())
			return
		}
		serverInfo := player.CurrentServer().Server().ServerInfo()
		observability.Info("gate_player_connected", "username", player.Username(), "server", serverInfo.Name(), "upstream", serverInfo.Addr().String())
	})
	event.Subscribe(p.Event(), 0, func(e *javaproxy.KickedFromServerEvent) {
		serverInfo := e.Server().ServerInfo()
		observability.Warn("gate_backend_rejected_player",
			"username", e.Player().Username(),
			"server", serverInfo.Name(),
			"upstream", serverInfo.Addr().String(),
			"during_server_connect", e.KickedDuringServerConnect(),
			"reason", e.OriginalReason(),
		)
	})

	return r, nil
}

func (r *Runtime) resolvePing(host string) *ping.ServerPing {
	r.mu.RLock()
	if cached, ok := r.pingCache[host]; ok && time.Now().Before(cached.expiresAt) {
		payload := *cached.payload
		r.mu.RUnlock()
		return &payload
	}
	r.mu.RUnlock()
	existing := r.pickInitialServer(host)

	payload := r.fallbackPing(existing != nil)
	if existing != nil {
		observability.Info("gate_ping_host_matched", "host", host, "selected_server", existing.ServerInfo().Name(), "selected_upstream", existing.ServerInfo().Addr().String())
		r.mu.Lock()
		r.pingCache[host] = cachedPing{payload: payload, expiresAt: time.Now().Add(r.pingCacheTTL)}
		r.mu.Unlock()
	} else {
		observability.Warn("gate_ping_host_unmatched", "host", host)
	}
	return payload
}

func (r *Runtime) fallbackPing(exists bool) *ping.ServerPing {
	description := r.pingFallback.motd
	players := &ping.Players{Online: 0, Max: 0}
	if !exists {
		description = r.pingFallback.offlineMotd
		players = &ping.Players{Online: 0, Max: 0, Sample: []ping.SamplePlayer{{Name: "UNAVAILABLE", ID: [16]byte{}}}}
	}
	sp := &ping.ServerPing{
		Description: &component.Text{Content: description},
		Players:     players,
		Version:     ping.Version{Name: "mcproxy", Protocol: 0},
	}
	if r.pingFallback.favicon != "" {
		sp.Favicon = favicon.Favicon(r.pingFallback.favicon)
	}
	return sp
}

func hostnameFromAddr(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err == nil {
		return strings.ToLower(strings.TrimSpace(host))
	}
	return strings.ToLower(strings.TrimSpace(addr.String()))
}

func (r *Runtime) Start(ctx context.Context) error {
	if err := r.syncServers(ctx); err != nil {
		return err
	}
	go r.syncLoop(ctx)

	r.mu.RLock()
	p := r.proxy
	r.mu.RUnlock()
	if p == nil {
		return fmt.Errorf("gate proxy not initialized")
	}
	observability.Info("gate_starting", "bind", r.bind)
	ctx = logr.NewContext(ctx, logr.FromSlogHandler(observability.L().Handler()))
	return p.Start(ctx)
}

func (r *Runtime) Bind() string {
	return r.bind
}

func (r *Runtime) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(r.syncEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.syncServers(ctx); err != nil {
				observability.Warn("gate_sync_servers_failed", "error", err)
			}
		}
	}
}

func (r *Runtime) syncServers(ctx context.Context) error {
	r.mu.RLock()
	p := r.proxy
	st := r.store
	r.mu.RUnlock()
	if p == nil || st == nil {
		return fmt.Errorf("gate runtime not initialized")
	}

	servers, err := st.ListServers(ctx)
	if err != nil {
		return err
	}

	desired := make(map[string]string, len(servers))
	tryOrder := make([]string, 0, len(servers))
	routes := make(map[string]string, len(servers))
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		name := strings.TrimSpace(s.Name)
		upstream := strings.TrimSpace(s.Upstream)
		if name == "" || upstream == "" {
			continue
		}
		desired[strings.ToLower(name)] = upstream
		tryOrder = append(tryOrder, name)
		host := strings.ToLower(strings.TrimSpace(s.PublicHost))
		if host != "" {
			routes[host] = name
		}
	}
	if len(tryOrder) == 0 {
		return fmt.Errorf("no enabled backend servers registered")
	}

	for _, registered := range p.Servers() {
		info := registered.ServerInfo()
		name := strings.ToLower(info.Name())
		upstream, ok := desired[name]
		if !ok || info.Addr().String() != upstream {
			p.Unregister(info)
		}
	}
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		name := strings.TrimSpace(s.Name)
		upstream := strings.TrimSpace(s.Upstream)
		if name == "" || upstream == "" {
			continue
		}
		current := p.Server(name)
		if current != nil && current.ServerInfo().Addr().String() == upstream {
			continue
		}
		addr, aerr := net.ResolveTCPAddr("tcp", upstream)
		if aerr != nil {
			return fmt.Errorf("resolve upstream for %s: %w", name, aerr)
		}
		if _, err := p.Register(javaproxy.NewServerInfo(name, addr)); err != nil && err != javaproxy.ErrServerAlreadyExists {
			return fmt.Errorf("register backend %s: %w", name, err)
		}
	}

	r.mu.Lock()
	r.tryOrder = tryOrder
	r.routes = routes
	r.pingCache = make(map[string]cachedPing)
	r.mu.Unlock()
	observability.Info("gate_routes_synced", "route_count", len(routes), "try_order_count", len(tryOrder))
	return nil
}

func (r *Runtime) pickInitialServer(host string) javaproxy.RegisteredServer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.proxy == nil {
		observability.Warn("gate_pick_server_proxy_nil", "host", host)
		return nil
	}
	if target, ok := r.routes[strings.ToLower(strings.TrimSpace(host))]; ok {
		if server := r.proxy.Server(target); server != nil {
			observability.Info("gate_pick_server_route_hit", "host", host, "target", target, "upstream", server.ServerInfo().Addr().String())
			return server
		}
		observability.Warn("gate_pick_server_route_target_missing", "host", host, "target", target)
	} else {
		observability.Warn("gate_pick_server_route_miss", "host", host)
	}
	for _, name := range r.tryOrder {
		if server := r.proxy.Server(name); server != nil {
			observability.Warn("gate_pick_server_fallback_try_order", "host", host, "fallback_server", name, "upstream", server.ServerInfo().Addr().String())
			return server
		}
	}
	servers := r.proxy.Servers()
	if len(servers) == 0 {
		observability.Error("gate_pick_server_no_registered_servers", "host", host)
		return nil
	}
	observability.Warn("gate_pick_server_fallback_first", "host", host, "fallback_server", servers[0].ServerInfo().Name(), "upstream", servers[0].ServerInfo().Addr().String())
	return servers[0]
}

func boolPtr(v bool) *bool { return &v }

func netIPString(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err == nil {
		return host
	}
	return strings.TrimSpace(addr.String())
}
