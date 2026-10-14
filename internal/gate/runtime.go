package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/robinbraemer/event"
	"go.minekube.com/common/minecraft/component"
	gatecfg "go.minekube.com/gate/pkg/edition/java/config"
	"go.minekube.com/gate/pkg/edition/java/ping"
	"go.minekube.com/gate/pkg/edition/java/profile"
	javaproxy "go.minekube.com/gate/pkg/edition/java/proxy"
	"go.minekube.com/gate/pkg/edition/java/proxy/phase"
	"go.minekube.com/gate/pkg/util/favicon"
	"go.minekube.com/gate/pkg/util/netutil"
	gateuuid "go.minekube.com/gate/pkg/util/uuid"

	"mcproxy/internal/config"
	"mcproxy/internal/geo"
	"mcproxy/internal/observability"
	"mcproxy/internal/store"
)

type Runtime struct {
	mu             sync.RWMutex
	proxy          *javaproxy.Proxy
	bind           string
	forwardingMode gatecfg.ForwardingMode
	store          *store.Store
	geo            *geo.Service
	tryOrder       []string
	routes         map[string]routeTarget
	serverIDs      map[string]int
	pingMOTD       map[string]string
	syncEvery      time.Duration
	pingCacheTTL   time.Duration
	pingFallback   pingFallback
	pingCache      map[string]cachedPing
	loginMu        sync.Mutex
	pendingLogins  map[gateuuid.UUID]pendingLogin
	pendingByIP    map[string]int
	maxLoginsPerIP int
	ready          atomic.Bool
}

type pendingLogin struct {
	ip          string
	distributed bool
}

type routeTarget struct {
	name     string
	serverID int
}

// bungeeGuardServerInfo carries the token belonging to one backend. Gate's
// public ServerDialer hook lets us rewrite the first backend handshake while
// retaining Gate's own BungeeGuard/Forge handling. BungeeGuard validates the
// bungeeguard-token profile property on the backend.
type bungeeGuardServerInfo struct {
	javaproxy.ServerInfo
	token string
}

// forwardingServerInfo overrides Gate's process-wide forwarding fallback for
// one backend. This is used for per-server none/legacy/BungeeGuard selection;
// Velocity modern forwarding still uses Gate's global login-plugin flow.
type forwardingServerInfo struct {
	javaproxy.ServerInfo
	mode  gatecfg.ForwardingMode
	token string
}

func (i *forwardingServerInfo) HandshakeAddr(defaultHost string, player javaproxy.Player) string {
	switch i.mode {
	case gatecfg.NoneForwardingMode:
		return defaultHost
	case gatecfg.LegacyForwardingMode:
		return bungeeGuardHandshakeAddrWithExtraData(i.Addr().String(), player.RemoteAddr(), player.GameProfile(), "", forgeExtraDataProperty(player))
	case gatecfg.BungeeGuardForwardingMode:
		return bungeeGuardHandshakeAddrWithExtraData(i.Addr().String(), player.RemoteAddr(), player.GameProfile(), i.token, forgeExtraDataProperty(player))
	default:
		return defaultHost
	}
}

func (i *forwardingServerInfo) ForwardingMode() gatecfg.ForwardingMode {
	return i.mode
}

func (i *bungeeGuardServerInfo) ForwardingMode() gatecfg.ForwardingMode {
	return gatecfg.BungeeGuardForwardingMode
}

func (i *bungeeGuardServerInfo) Dial(ctx context.Context, player javaproxy.Player) (net.Conn, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", i.Addr().String())
	if err != nil {
		return nil, err
	}
	return &bungeeGuardHandshakeConn{
		Conn:     conn,
		player:   player,
		upstream: i.Addr().String(),
		token:    i.token,
	}, nil
}

// bungeeGuardHandshakeConn rewrites only the first backend handshake. Gate's
// built-in BungeeGuard mode still builds the packet (and therefore preserves
// usedForwarding=true for Forge), while this adapter replaces its global token
// with the token belonging to the selected backend.
type bungeeGuardHandshakeConn struct {
	net.Conn
	mu        sync.Mutex
	player    javaproxy.Player
	upstream  string
	token     string
	pending   []byte
	rewritten bool
}

func (c *bungeeGuardHandshakeConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rewritten {
		return c.Conn.Write(p)
	}
	c.pending = append(c.pending, p...)
	rewritten, complete := c.rewritePending()
	if !complete {
		return len(p), nil
	}
	c.rewritten = true
	c.pending = nil
	if err := writeAll(c.Conn, rewritten); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *bungeeGuardHandshakeConn) rewritePending() ([]byte, bool) {
	return rewriteBungeeGuardHandshake(c.pending, c.upstream, c.player.RemoteAddr(), c.player.GameProfile(), c.token, forgeExtraDataProperty(c.player))
}

func rewriteBungeeGuardHandshake(data []byte, upstream string, remote net.Addr, gp profile.GameProfile, token, extraData string) ([]byte, bool) {
	frameLength, prefixLength, ok := readVarInt(data)
	if !ok {
		return nil, false
	}
	if frameLength < 0 || prefixLength+frameLength > len(data) {
		return nil, false
	}
	frame := data[prefixLength : prefixLength+frameLength]
	position := 0
	if _, n, ok := readVarIntAt(frame, position); !ok { // packet id
		return nil, false
	} else {
		position += n
	}
	if _, n, ok := readVarIntAt(frame, position); !ok { // protocol version
		return nil, false
	} else {
		position += n
	}
	hostLength, n, ok := readVarIntAt(frame, position)
	if !ok || hostLength < 0 || position+n+hostLength > len(frame) {
		return nil, false
	}
	hostLengthStart := position
	hostStart := position + n
	hostEnd := hostStart + hostLength
	forwarded := bungeeGuardHandshakeAddrWithExtraData(
		upstream,
		remote,
		gp,
		token,
		extraData,
	)
	newHost := []byte(forwarded)
	body := make([]byte, 0, len(frame)-hostLength+len(newHost)+5)
	body = append(body, frame[:hostLengthStart]...)
	body = append(body, encodeVarInt(len(newHost))...)
	body = append(body, newHost...)
	body = append(body, frame[hostEnd:]...)
	out := make([]byte, 0, len(body)+5+len(data)-(prefixLength+frameLength))
	out = append(out, encodeVarInt(len(body))...)
	out = append(out, body...)
	out = append(out, data[prefixLength+frameLength:]...)
	return out, true
}

func writeAll(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return fmt.Errorf("backend connection wrote zero bytes")
		}
		data = data[n:]
	}
	return nil
}

func readVarInt(data []byte) (value, width int, ok bool) {
	return readVarIntAt(data, 0)
}

func readVarIntAt(data []byte, offset int) (value, width int, ok bool) {
	var result int
	for i := 0; i < 5; i++ {
		if offset+i >= len(data) {
			return 0, 0, false
		}
		b := data[offset+i]
		result |= int(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return result, i + 1, true
		}
	}
	return 0, 0, false
}

func encodeVarInt(value int) []byte {
	if value < 0 {
		value = 0
	}
	out := make([]byte, 0, 5)
	for {
		b := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if value == 0 {
			return out
		}
	}
}

func bungeeGuardHandshakeAddr(upstream string, remote net.Addr, gp profile.GameProfile, token string) string {
	return bungeeGuardHandshakeAddrWithExtraData(upstream, remote, gp, token, "")
}

func bungeeGuardHandshakeAddrWithExtraData(upstream string, remote net.Addr, gp profile.GameProfile, token, extraData string) string {
	properties := append([]profile.Property(nil), gp.Properties...)
	if extraData != "" {
		properties = append(properties, profile.Property{Name: "extraData", Value: extraData})
	}
	properties = append(properties, profile.Property{Name: "bungeeguard-token", Value: token})
	props, err := json.Marshal(properties)
	if err != nil {
		return upstream
	}
	return strings.Join([]string{
		upstream,
		netutil.Host(remote),
		gp.ID.Undashed(),
		string(props),
	}, "\x00")
}

// Gate's Player interface intentionally exposes Minecraft behavior rather
// than the concrete connection type. The concrete connected player still
// promotes Type through MinecraftConn, so use the narrow optional interface
// here to preserve Gate's Forge/BungeeGuard compatibility behavior.
func forgeExtraDataProperty(player javaproxy.Player) string {
	typed, ok := player.(interface{ Type() phase.ConnectionType })
	if !ok {
		return ""
	}
	switch typed.Type() {
	case phase.ModernForge:
		vHost := netutil.Host(player.VirtualHost())
		for _, part := range strings.Split(vHost, "\x00") {
			if strings.HasPrefix(part, "FML2") || strings.HasPrefix(part, "FML3") || strings.HasPrefix(part, "FORGE") {
				return "\x01" + part
			}
		}
	case phase.LegacyForge:
		return "\x01FML\x00"
	}
	return ""
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
	Geo        *geo.Service
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
	cfg := gatecfg.DefaultConfig
	cfg.Bind = opts.Bind
	cfg.OnlineMode = opts.OnlineMode
	cfg.ProxyProtocol = opts.Config.GateProxyProtocol
	cfg.ProxyProtocolBackend = opts.Config.GateProxyProtocolBackend
	cfg.ProxyProtocolTrustedProxies = append([]string(nil), opts.Config.GateProxyProtocolTrustedProxies...)
	forwardingMode := strings.ToLower(strings.TrimSpace(opts.Config.GateForwardingMode))
	if forwardingMode == "" {
		forwardingMode = string(gatecfg.LegacyForwardingMode)
	}
	cfg.Forwarding.Mode = gatecfg.ForwardingMode(forwardingMode)
	cfg.Forwarding.VelocitySecret = opts.Config.GateVelocitySecret
	switch cfg.Forwarding.Mode {
	case gatecfg.NoneForwardingMode, gatecfg.LegacyForwardingMode:
	case gatecfg.VelocityForwardingMode:
		if strings.TrimSpace(cfg.Forwarding.VelocitySecret) == "" {
			return nil, fmt.Errorf("Gate velocity forwarding requires MCPROXY_GATE_VELOCITY_SECRET")
		}
	case gatecfg.BungeeGuardForwardingMode:
		return nil, fmt.Errorf("Gate bungeeguard forwarding must be configured per server; use forwarding_mode=bungeeguard on each backend")
	default:
		return nil, fmt.Errorf("unsupported Gate forwarding mode %q", forwardingMode)
	}
	for _, s := range servers {
		mode := strings.ToLower(strings.TrimSpace(s.ForwardingMode))
		if mode == "" {
			continue
		}
		if !isForwardingMode(mode) {
			return nil, fmt.Errorf("server %q has unsupported forwarding mode %q", s.Name, mode)
		}
		if mode == string(gatecfg.VelocityForwardingMode) && cfg.Forwarding.Mode != gatecfg.VelocityForwardingMode {
			return nil, fmt.Errorf("per-server velocity forwarding requires MCPROXY_GATE_FORWARDING_MODE=velocity")
		}
		if cfg.Forwarding.Mode == gatecfg.VelocityForwardingMode && mode != string(gatecfg.VelocityForwardingMode) {
			return nil, fmt.Errorf("per-server forwarding mode %q cannot be mixed with global velocity forwarding", mode)
		}
	}
	cfg.Servers = map[string]string{}
	cfg.Try = make([]string, 0, len(servers))
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		mode := strings.ToLower(strings.TrimSpace(s.ForwardingMode))
		if mode == "" {
			mode = forwardingMode
			if mode == string(gatecfg.BungeeGuardForwardingMode) {
				mode = string(gatecfg.LegacyForwardingMode)
			}
		}
		// Gate's static startup registration can safely retain servers whose
		// effective mode is the global mode. Per-server overrides must remain
		// API-registered so their handshake/dial wrappers survive Proxy.init.
		if mode != forwardingMode || mode == string(gatecfg.BungeeGuardForwardingMode) {
			continue
		}
		cfg.Servers[s.Name] = s.Upstream
		cfg.Try = append(cfg.Try, s.Name)
	}
	eventMgr := event.New()
	p, err := javaproxy.New(javaproxy.Options{Config: &cfg, EventMgr: eventMgr})
	if err != nil {
		return nil, err
	}

	r := &Runtime{
		proxy:          p,
		bind:           opts.Bind,
		forwardingMode: cfg.Forwarding.Mode,
		store:          opts.Store,
		geo:            opts.Geo,
		syncEvery:      5 * time.Second,
		tryOrder:       append([]string(nil), cfg.Try...),
		routes:         make(map[string]routeTarget),
		serverIDs:      make(map[string]int),
		pingCacheTTL:   time.Duration(opts.Config.PingCacheTTL) * time.Second,
		pingFallback:   pingFallback{motd: opts.Config.PingFallbackMotd, offlineMotd: opts.Config.PingOfflineMotd, favicon: opts.Config.PingFallbackFavicon},
		pingCache:      make(map[string]cachedPing),
		pingMOTD:       make(map[string]string),
		pendingLogins:  make(map[gateuuid.UUID]pendingLogin),
		pendingByIP:    make(map[string]int),
		maxLoginsPerIP: opts.Config.GateMaxConcurrentLoginsPerIP,
	}
	event.Subscribe(p.Event(), 0, func(e *javaproxy.PreLoginEvent) {
		remoteIP := netIPString(e.Conn().RemoteAddr())
		playerID, hasPlayerID := e.ID()
		reserved, reserveErr := r.reserveLoginAttempt(remoteIP, playerID, hasPlayerID)
		if reserveErr != nil {
			observability.Error("gate_prelogin_concurrent_limit_failed", "error", reserveErr, "remote_ip", remoteIP, "username", e.Username())
			e.Deny(&component.Text{Content: "Security protection is temporarily unavailable"})
			return
		}
		if !reserved && r.maxLoginsPerIP > 0 && hasPlayerID {
			observability.Warn("gate_prelogin_concurrent_limit", "remote_ip", remoteIP, "username", e.Username(), "limit", r.maxLoginsPerIP)
			e.Deny(&component.Text{Content: "Too many concurrent logins from this address"})
			return
		}
		var serverID *int
		if target, ok := r.routeTargetForAddr(e.Conn().VirtualHost()); ok {
			id := target.serverID
			serverID = &id
		}
		res, err := r.evaluateAttempt(context.Background(), serverID, remoteIP, e.Username(), true)
		if err != nil {
			if reserved {
				r.releaseLogin(playerID)
			}
			observability.Error("gate_prelogin_evaluate_failed", "error", err, "remote_ip", remoteIP, "username", e.Username())
			e.Deny(&component.Text{Content: "Security policy evaluation failed"})
			return
		}
		if !res.Allowed {
			if reserved {
				r.releaseLogin(playerID)
			}
			observability.Warn("gate_prelogin_denied", "remote_ip", remoteIP, "username", e.Username(), "reason", res.Reason)
			e.Deny(&component.Text{Content: "Connection denied: " + res.Reason})
		}
	})

	// Re-check policy after authentication/profile setup. Counters are not
	// incremented here because PreLogin already recorded the connection.
	event.Subscribe(p.Event(), 0, func(e *javaproxy.LoginEvent) {
		remoteIP := netIPString(e.Player().RemoteAddr())
		var serverID *int
		if target, ok := r.routeTargetForAddr(e.Player().VirtualHost()); ok {
			id := target.serverID
			serverID = &id
		}
		res, err := r.evaluateAttempt(context.Background(), serverID, remoteIP, e.Player().Username(), false)
		if err != nil {
			r.releaseLogin(e.Player().ID())
			observability.Error("gate_login_evaluate_failed", "error", err, "remote_ip", remoteIP, "username", e.Player().Username())
			e.Deny(&component.Text{Content: "Security policy evaluation failed"})
			return
		}
		if !res.Allowed {
			r.releaseLogin(e.Player().ID())
			observability.Warn("gate_login_denied", "remote_ip", remoteIP, "username", e.Player().Username(), "reason", res.Reason)
			e.Deny(&component.Text{Content: "Connection denied: " + res.Reason})
		}
	})
	event.Subscribe(p.Event(), 0, func(e *javaproxy.PostLoginEvent) {
		// Limit authentication/backend setup, not already-connected players.
		r.releaseLogin(e.Player().ID())
	})

	// Re-evaluate server-scoped rules when a connected player switches backend
	// servers. Counters are not incremented here so a player moving between
	// backends cannot consume a connection burst budget repeatedly.
	event.Subscribe(p.Event(), 0, func(e *javaproxy.ServerPreConnectEvent) {
		server := e.Server()
		if server == nil {
			return
		}
		serverID := r.serverIDByName(server.ServerInfo().Name())
		remoteIP := netIPString(e.Player().RemoteAddr())
		res, err := r.evaluateAttempt(context.Background(), serverID, remoteIP, e.Player().Username(), false)
		if err != nil {
			observability.Error("gate_server_preconnect_evaluate_failed", "error", err, "remote_ip", remoteIP, "username", e.Player().Username(), "server", server.ServerInfo().Name())
			e.Deny()
			return
		}
		if !res.Allowed {
			observability.Warn("gate_server_preconnect_denied", "remote_ip", remoteIP, "username", e.Player().Username(), "server", server.ServerInfo().Name(), "reason", res.Reason)
			e.Deny()
		}
	})

	event.Subscribe(p.Event(), 0, func(e *javaproxy.PingEvent) {
		host := hostnameFromAddr(e.Connection().VirtualHost())
		observability.Info("gate_ping", "remote_ip", netIPString(e.Connection().RemoteAddr()), "host", host)
		e.SetPing(r.resolvePing(host))
	})

	event.Subscribe(p.Event(), 0, func(e *javaproxy.PlayerChooseInitialServerEvent) {
		host := hostnameFromAddr(e.Player().VirtualHost())
		if initial := r.pickInitialServerForAddr(e.Player().VirtualHost()); initial != nil {
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
		serverID := r.serverIDByName(serverInfo.Name())
		_ = r.store.RecordAudit(context.Background(), "gate", serverID, "connect.connected", map[string]any{"username": player.Username(), "server": serverInfo.Name(), "upstream": serverInfo.Addr().String()})
	})
	event.Subscribe(p.Event(), 0, func(e *javaproxy.DisconnectEvent) {
		player := e.Player()
		r.releaseLogin(player.ID())
		details := map[string]any{"username": player.Username(), "login_status": e.LoginStatus()}
		var serverID *int
		if current := player.CurrentServer(); current != nil {
			info := current.Server().ServerInfo()
			details["server"] = info.Name()
			details["upstream"] = info.Addr().String()
			serverID = r.serverIDByName(info.Name())
		}
		observability.Info("gate_player_disconnected", "username", player.Username(), "login_status", e.LoginStatus())
		_ = r.store.RecordAudit(context.Background(), "gate", serverID, "connect.disconnected", details)
	})
	event.Subscribe(p.Event(), 0, func(e *javaproxy.KickedFromServerEvent) {
		serverInfo := e.Server().ServerInfo()
		reason := componentReasonText(e.OriginalReason())
		observability.Warn("gate_backend_rejected_player",
			"username", e.Player().Username(),
			"server", serverInfo.Name(),
			"upstream", serverInfo.Addr().String(),
			"during_server_connect", e.KickedDuringServerConnect(),
			"reason", reason,
		)
		_ = r.store.RecordAudit(context.Background(), "gate", nil, "connect.backend_rejected", map[string]any{"username": e.Player().Username(), "server": serverInfo.Name(), "reason": reason})
	})

	return r, nil
}

// reserveLogin tracks only modern login attempts that expose a stable UUID.
// Older clients do not provide one during PreLogin, so the optional limit is
// intentionally bypassed for them instead of risking another player's slot.
func (r *Runtime) reserveLogin(ip string, id gateuuid.UUID, hasID bool) bool {
	if r.maxLoginsPerIP <= 0 || !hasID || id == gateuuid.Nil {
		return true
	}
	r.loginMu.Lock()
	defer r.loginMu.Unlock()
	if _, exists := r.pendingLogins[id]; exists {
		return true
	}
	if r.pendingByIP[ip] >= r.maxLoginsPerIP {
		return false
	}
	r.pendingLogins[id] = pendingLogin{ip: ip}
	r.pendingByIP[ip]++
	return true
}

func (r *Runtime) reserveLoginAttempt(ip string, id gateuuid.UUID, hasID bool) (bool, error) {
	reserved := r.reserveLogin(ip, id, hasID)
	if !reserved || r.maxLoginsPerIP <= 0 || !hasID || id == gateuuid.Nil || r.store == nil || !r.store.DistributedLoginReservations() {
		return reserved, nil
	}
	allowed, err := r.store.ReserveConcurrentLogin(context.Background(), ip, id.String(), r.maxLoginsPerIP)
	if err != nil || !allowed {
		r.releaseLogin(id)
		return false, err
	}
	r.loginMu.Lock()
	if pending, exists := r.pendingLogins[id]; exists {
		pending.distributed = true
		r.pendingLogins[id] = pending
	}
	r.loginMu.Unlock()
	return true, nil
}

func (r *Runtime) releaseLogin(id gateuuid.UUID) {
	if id == gateuuid.Nil {
		return
	}
	r.loginMu.Lock()
	defer r.loginMu.Unlock()
	pending, exists := r.pendingLogins[id]
	if !exists {
		return
	}
	delete(r.pendingLogins, id)
	if count := r.pendingByIP[pending.ip] - 1; count > 0 {
		r.pendingByIP[pending.ip] = count
	} else {
		delete(r.pendingByIP, pending.ip)
	}
	if pending.distributed && r.store != nil {
		if err := r.store.ReleaseConcurrentLogin(context.Background(), pending.ip, id.String()); err != nil {
			observability.Warn("gate_concurrent_login_release_failed", "error", err, "remote_ip", pending.ip)
		}
	}
}

func componentReasonText(reason component.Component) string {
	switch value := reason.(type) {
	case nil:
		return ""
	case *component.Text:
		if value == nil {
			return ""
		}
		return value.Content
	case *component.Translation:
		if value == nil {
			return ""
		}
		return value.Key
	default:
		return fmt.Sprintf("%T", reason)
	}
}

func (r *Runtime) evaluateAttempt(ctx context.Context, serverID *int, remoteIP, nickname string, record bool) (*store.EvaluateAttemptResult, error) {
	country := ""
	asn := ""
	if parsed := net.ParseIP(strings.TrimSpace(remoteIP)); parsed != nil && r.geo != nil {
		country = r.geo.CountryCode(parsed)
		asn = r.geo.ASN(parsed)
	}
	return r.store.EvaluateAttempt(ctx, store.EvaluateAttemptInput{
		ServerID: serverID,
		IP:       remoteIP,
		Nickname: nickname,
		Country:  country,
		ASN:      asn,
		Record:   boolPtr(record),
	})
}

func (r *Runtime) serverIDByName(name string) *int {
	r.mu.RLock()
	id, ok := r.serverIDs[strings.ToLower(strings.TrimSpace(name))]
	r.mu.RUnlock()
	if ok {
		return &id
	}
	return nil
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

	serverName := ""
	if existing != nil {
		serverName = strings.ToLower(existing.ServerInfo().Name())
	}
	r.mu.RLock()
	motd := r.pingMOTD[serverName]
	r.mu.RUnlock()
	payload := r.fallbackPing(existing != nil, motd)
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

func (r *Runtime) fallbackPing(exists bool, serverMOTD string) *ping.ServerPing {
	description := r.pingFallback.motd
	if strings.TrimSpace(serverMOTD) != "" {
		description = strings.TrimSpace(serverMOTD)
	}
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
	r.ready.Store(true)
	defer r.ready.Store(false)

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

// Ready reports whether the Gate listener has successfully entered its run
// loop. It is safe to call from the HTTP readiness handler.
func (r *Runtime) Ready() bool {
	return r != nil && r.ready.Load()
}

func (r *Runtime) Bind() string {
	return r.bind
}

// PlayerCount returns the number of players currently connected to Gate.
func (r *Runtime) PlayerCount() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	p := r.proxy
	r.mu.RUnlock()
	if p == nil {
		return 0
	}
	return p.PlayerCount()
}

// BackendCount returns the number of backend servers currently registered in Gate.
func (r *Runtime) BackendCount() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	p := r.proxy
	r.mu.RUnlock()
	if p == nil {
		return 0
	}
	return len(p.Servers())
}

func (r *Runtime) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(r.syncEvery)
	defer ticker.Stop()
	invalidations := r.store.Invalidations()
	for {
		select {
		case <-ctx.Done():
			return
		case <-invalidations:
			if err := r.syncServers(ctx); err != nil {
				observability.Warn("gate_invalidation_sync_failed", "error", err)
			}
		case <-ticker.C:
			if err := r.syncServers(ctx); err != nil {
				observability.Warn("gate_sync_servers_failed", "error", err)
			}
		}
	}
}

func (r *Runtime) serverForwardingMode(s store.ServerDTO) gatecfg.ForwardingMode {
	mode := strings.ToLower(strings.TrimSpace(s.ForwardingMode))
	if mode == "" {
		// BungeeGuard is intentionally never inherited from the process-wide
		// fallback. It is a per-backend credential and must be selected on the
		// server record to avoid accidentally sending one backend's token mode
		// to every backend after an old environment setting is left behind.
		if r.forwardingMode == gatecfg.BungeeGuardForwardingMode {
			return gatecfg.LegacyForwardingMode
		}
		return r.forwardingMode
	}
	return gatecfg.ForwardingMode(mode)
}

func isForwardingMode(mode string) bool {
	switch gatecfg.ForwardingMode(strings.ToLower(strings.TrimSpace(mode))) {
	case gatecfg.NoneForwardingMode, gatecfg.LegacyForwardingMode, gatecfg.VelocityForwardingMode, gatecfg.BungeeGuardForwardingMode:
		return true
	default:
		return false
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
	desiredTokens := make(map[string]string, len(servers))
	desiredModes := make(map[string]gatecfg.ForwardingMode, len(servers))
	resolved := make(map[string]*net.TCPAddr, len(servers))
	tryOrder := make([]string, 0, len(servers))
	routes := make(map[string]routeTarget, len(servers))
	serverIDs := make(map[string]int, len(servers))
	sort.SliceStable(servers, func(i, j int) bool {
		if servers[i].Priority != servers[j].Priority {
			return servers[i].Priority < servers[j].Priority
		}
		return strings.ToLower(servers[i].Name) < strings.ToLower(servers[j].Name)
	})
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		name := strings.TrimSpace(s.Name)
		upstream := strings.TrimSpace(s.Upstream)
		if name == "" || upstream == "" {
			continue
		}
		addr, aerr := net.ResolveTCPAddr("tcp", upstream)
		if aerr != nil {
			return fmt.Errorf("resolve upstream for %s: %w", name, aerr)
		}
		desired[strings.ToLower(name)] = upstream
		desiredTokens[strings.ToLower(name)] = s.BungeeGuardAllowedToken
		desiredModes[strings.ToLower(name)] = r.serverForwardingMode(s)
		resolved[strings.ToLower(name)] = addr
		serverIDs[strings.ToLower(name)] = s.ID
		tryOrder = append(tryOrder, name)
		target := routeTarget{name: name, serverID: s.ID}
		for _, key := range routeKeys(s.PublicHost, s.PublicAddr) {
			if previous, exists := routes[key]; exists {
				observability.Warn("gate_route_collision", "route_key", key, "selected_server", previous.name, "ignored_server", name)
				continue
			}
			routes[key] = target
		}
	}
	if len(tryOrder) == 0 {
		// Keep the Gate listener alive while the control plane has no enabled
		// backends. A later server mutation can then be picked up by syncLoop
		// without requiring a process restart.
		for _, registered := range p.Servers() {
			p.Unregister(registered.ServerInfo())
		}
		r.mu.Lock()
		r.tryOrder = nil
		r.routes = routes
		r.serverIDs = serverIDs
		r.pingCache = make(map[string]cachedPing)
		r.pingMOTD = make(map[string]string)
		r.mu.Unlock()
		observability.Warn("gate_routes_empty", "reason", "no enabled backend servers")
		return nil
	}

	for _, registered := range p.Servers() {
		info := registered.ServerInfo()
		name := strings.ToLower(info.Name())
		upstream, ok := desired[name]
		tokenChanged := false
		modeChanged := false
		mode := desiredModes[name]
		if mode == gatecfg.BungeeGuardForwardingMode {
			current, isPerServer := info.(*bungeeGuardServerInfo)
			tokenChanged = !isPerServer || current.token != desiredTokens[name]
		}
		if wrapped, isWrapped := info.(*forwardingServerInfo); isWrapped {
			modeChanged = wrapped.mode != mode || (mode == gatecfg.BungeeGuardForwardingMode && wrapped.token != desiredTokens[name])
		} else if mode != r.forwardingMode && mode != gatecfg.VelocityForwardingMode {
			modeChanged = true
		}
		if !ok || info.Addr().String() != upstream || tokenChanged || modeChanged {
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
		mode := r.serverForwardingMode(s)
		if current != nil && current.ServerInfo().Addr().String() == upstream {
			if mode == r.forwardingMode && mode != gatecfg.BungeeGuardForwardingMode {
				continue
			}
			if wrapped, ok := current.ServerInfo().(*bungeeGuardServerInfo); ok && mode == gatecfg.BungeeGuardForwardingMode && wrapped.token == s.BungeeGuardAllowedToken {
				continue
			}
			if wrapped, ok := current.ServerInfo().(*forwardingServerInfo); ok && wrapped.mode == mode && (mode != gatecfg.BungeeGuardForwardingMode || wrapped.token == s.BungeeGuardAllowedToken) {
				continue
			}
		}
		addr := resolved[strings.ToLower(name)]
		if addr == nil {
			return fmt.Errorf("resolved upstream missing for %s", name)
		}
		info := javaproxy.NewServerInfo(name, addr)
		if mode == gatecfg.BungeeGuardForwardingMode && mode == r.forwardingMode {
			info = &bungeeGuardServerInfo{ServerInfo: info, token: s.BungeeGuardAllowedToken}
		} else if mode != r.forwardingMode {
			info = &forwardingServerInfo{ServerInfo: info, mode: mode, token: s.BungeeGuardAllowedToken}
		}
		if _, err := p.Register(info); err != nil && err != javaproxy.ErrServerAlreadyExists {
			return fmt.Errorf("register backend %s: %w", name, err)
		}
	}

	r.mu.Lock()
	r.tryOrder = tryOrder
	r.routes = routes
	r.serverIDs = serverIDs
	r.pingMOTD = make(map[string]string, len(servers))
	for _, s := range servers {
		if s.Enabled {
			r.pingMOTD[strings.ToLower(strings.TrimSpace(s.Name))] = s.PingMOTD
		}
	}
	r.pingCache = make(map[string]cachedPing)
	r.mu.Unlock()
	observability.Info("gate_routes_synced", "route_count", len(routes), "try_order_count", len(tryOrder))
	return nil
}

func (r *Runtime) pickInitialServer(host string) javaproxy.RegisteredServer {
	return r.pickInitialServerByKeys([]string{strings.ToLower(strings.TrimSpace(host))})
}

func (r *Runtime) pickInitialServerForAddr(addr net.Addr) javaproxy.RegisteredServer {
	keys := []string{strings.ToLower(strings.TrimSpace(addrString(addr))), hostnameFromAddr(addr)}
	return r.pickInitialServerByKeys(keys)
}

func (r *Runtime) pickInitialServerByKeys(keys []string) javaproxy.RegisteredServer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.proxy == nil {
		observability.Warn("gate_pick_server_proxy_nil", "keys", keys)
		return nil
	}
	for _, key := range keys {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		if target, ok := r.routes[key]; ok {
			if server := r.proxy.Server(target.name); server != nil {
				observability.Info("gate_pick_server_route_hit", "route_key", key, "target", target.name, "upstream", server.ServerInfo().Addr().String())
				return server
			}
			observability.Warn("gate_pick_server_route_target_missing", "route_key", key, "target", target.name)
		}
	}
	for _, name := range r.tryOrder {
		if server := r.proxy.Server(name); server != nil {
			observability.Warn("gate_pick_server_fallback_try_order", "route_keys", keys, "fallback_server", name, "upstream", server.ServerInfo().Addr().String())
			return server
		}
	}
	servers := r.proxy.Servers()
	if len(servers) == 0 {
		observability.Error("gate_pick_server_no_registered_servers", "route_keys", keys)
		return nil
	}
	observability.Warn("gate_pick_server_fallback_first", "route_keys", keys, "fallback_server", servers[0].ServerInfo().Name(), "upstream", servers[0].ServerInfo().Addr().String())
	return servers[0]
}

func (r *Runtime) routeTargetForAddr(addr net.Addr) (routeTarget, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, key := range []string{strings.ToLower(strings.TrimSpace(addrString(addr))), hostnameFromAddr(addr)} {
		if target, ok := r.routes[key]; ok && key != "" {
			return target, true
		}
	}
	return routeTarget{}, false
}

func routeKeys(publicHost, publicAddr string) []string {
	keys := make([]string, 0, 3)
	if host := strings.ToLower(strings.TrimSpace(publicHost)); host != "" {
		keys = append(keys, host)
	}
	if addr := strings.ToLower(strings.TrimSpace(publicAddr)); addr != "" {
		keys = append(keys, addr)
		if host, _, err := net.SplitHostPort(addr); err == nil {
			keys = append(keys, host)
		}
	}
	return keys
}

func addrString(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
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
