package gate

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/robinbraemer/event"
	"go.minekube.com/common/minecraft/component"
	gatecfg "go.minekube.com/gate/pkg/edition/java/config"
	javaproxy "go.minekube.com/gate/pkg/edition/java/proxy"

	"mcproxy/internal/observability"
	"mcproxy/internal/store"
)

type Runtime struct {
	mu        sync.RWMutex
	proxy     *javaproxy.Proxy
	bind      string
	store     *store.Store
	tryOrder  []string
	syncEvery time.Duration
}

type Options struct {
	Bind       string
	OnlineMode bool
	Store      *store.Store
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

	p, err := javaproxy.New(javaproxy.Options{Config: &cfg})
	if err != nil {
		return nil, err
	}

	r := &Runtime{proxy: p, bind: opts.Bind, store: opts.Store, syncEvery: 5 * time.Second, tryOrder: append([]string(nil), cfg.Try...)}
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
		observability.Info("gate_ping", "remote_ip", netIPString(e.Connection().RemoteAddr()))
	})

	event.Subscribe(p.Event(), 0, func(e *javaproxy.PlayerChooseInitialServerEvent) {
		if initial := r.pickInitialServer(); initial != nil {
			e.SetInitialServer(initial)
		}
	})

	return r, nil
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
	r.mu.Unlock()
	return nil
}

func (r *Runtime) pickInitialServer() javaproxy.RegisteredServer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.proxy == nil {
		return nil
	}
	for _, name := range r.tryOrder {
		if server := r.proxy.Server(name); server != nil {
			return server
		}
	}
	servers := r.proxy.Servers()
	if len(servers) == 0 {
		return nil
	}
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
