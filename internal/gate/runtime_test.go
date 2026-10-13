package gate

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"go.minekube.com/common/minecraft/component"
	gatecfg "go.minekube.com/gate/pkg/edition/java/config"
	"go.minekube.com/gate/pkg/edition/java/profile"
	javaproxy "go.minekube.com/gate/pkg/edition/java/proxy"
	"go.minekube.com/gate/pkg/util/netutil"
	"mcproxy/internal/config"
	"mcproxy/internal/store"
	"mcproxy/internal/testutil"
)

func TestBungeeGuardHandshakeUsesBackendToken(t *testing.T) {
	gp := profile.NewOffline("sugarcane_0")
	got := bungeeGuardHandshakeAddr("127.0.0.1:25566", netutil.NewAddr("198.51.100.20:41234", "tcp"), *gp, "0123456789abcdef0123456789abcdef")
	parts := strings.Split(got, "\x00")
	if len(parts) != 4 || parts[0] != "127.0.0.1:25566" || parts[1] != "198.51.100.20" {
		t.Fatalf("unexpected BungeeGuard handshake: %q", got)
	}
	var properties []profile.Property
	if err := json.Unmarshal([]byte(parts[3]), &properties); err != nil {
		t.Fatalf("decode forwarded properties: %v", err)
	}
	if len(properties) != 1 || properties[0].Name != "bungeeguard-token" || properties[0].Value != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected forwarded token properties: %#v", properties)
	}
}

func TestBungeeGuardHandshakePreservesForgeExtraData(t *testing.T) {
	gp := profile.NewOffline("forge_player")
	got := bungeeGuardHandshakeAddrWithExtraData("127.0.0.1:25566", netutil.NewAddr("198.51.100.20:41234", "tcp"), *gp, "token", "\x01FML3")
	parts := strings.Split(got, "\x00")
	if len(parts) != 4 {
		t.Fatalf("unexpected BungeeGuard handshake parts: %q", got)
	}
	var properties []profile.Property
	if err := json.Unmarshal([]byte(parts[3]), &properties); err != nil {
		t.Fatalf("decode forwarded properties: %v", err)
	}
	if len(properties) != 2 || properties[0].Name != "extraData" || properties[0].Value != "\x01FML3" || properties[1].Name != "bungeeguard-token" {
		t.Fatalf("unexpected Forge forwarding properties: %#v", properties)
	}
}

func TestRewriteBungeeGuardHandshakeReplacesOnlyHost(t *testing.T) {
	gp := profile.NewOffline("forge_player")
	originalHost := "127.0.0.1:25566\x00FML3\x00:25565"
	body := append(encodeVarInt(0), encodeVarInt(777)...)
	body = append(body, encodeVarInt(len(originalHost))...)
	body = append(body, originalHost...)
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, 25565)
	body = append(body, port...)
	body = append(body, encodeVarInt(2)...)
	packet := append(encodeVarInt(len(body)), body...)
	packet = append(packet, []byte("next-packet")...)

	got, complete := rewriteBungeeGuardHandshake(packet, "127.0.0.1:25566", netutil.NewAddr("198.51.100.20:41234", "tcp"), *gp, "token", "\x01FML3")
	if !complete {
		t.Fatal("expected complete handshake frame")
	}
	frameLength, prefix, ok := readVarInt(got)
	if !ok || prefix+frameLength > len(got) {
		t.Fatalf("invalid rewritten frame: %q", got)
	}
	frame := got[prefix : prefix+frameLength]
	position := 0
	_, n, _ := readVarIntAt(frame, position)
	position += n
	_, n, _ = readVarIntAt(frame, position)
	position += n
	hostLength, n, _ := readVarIntAt(frame, position)
	position += n
	forwardedHost := string(frame[position : position+hostLength])
	wantHost := bungeeGuardHandshakeAddrWithExtraData("127.0.0.1:25566", netutil.NewAddr("198.51.100.20:41234", "tcp"), *gp, "token", "\x01FML3")
	if forwardedHost != wantHost {
		t.Fatalf("rewritten host = %q, want %q", forwardedHost, wantHost)
	}
	if string(got[prefix+frameLength:]) != "next-packet" {
		t.Fatalf("rewriter did not preserve trailing packet: %q", got[prefix+frameLength:])
	}
}

func TestGateLoginRoutesHostnameToSelectedBackend(t *testing.T) {
	st := testutil.NewTestStore(t)
	backendA := startLoginCaptureBackend(t)
	backendB := startLoginCaptureBackend(t)
	hostA := "survival.example.test"
	hostB := "creative.example.test"
	if _, err := st.CreateServer(context.Background(), store.ServerInput{Name: "survival", Upstream: backendA.addr, PublicHost: &hostA}); err != nil {
		t.Fatalf("create survival server: %v", err)
	}
	if _, err := st.CreateServer(context.Background(), store.ServerInput{Name: "creative", Upstream: backendB.addr, PublicHost: &hostB}); err != nil {
		t.Fatalf("create creative server: %v", err)
	}

	gateAddr := freeTCPAddress(t)
	rt, err := New(context.Background(), Options{
		Bind: gateAddr, OnlineMode: false, Store: st,
		Config: config.Config{GateForwardingMode: "none"},
	})
	if err != nil {
		t.Fatalf("new Gate runtime: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan error, 1)
	go func() { started <- rt.Start(ctx) }()
	waitGateReady(t, rt)
	t.Cleanup(func() {
		for _, player := range rt.proxy.Players() {
			player.Disconnect(&component.Text{Content: "test cleanup"})
		}
		waitGatePlayersGone(t, rt.proxy)
		rt.proxy.Shutdown(&component.Text{Content: "test shutdown"})
		cancel()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Error("Gate runtime did not stop")
		}
	})

	loginThroughGate(t, gateAddr, hostB)
	select {
	case got := <-backendB.hosts:
		if got != hostB {
			t.Fatalf("creative backend received host %q, want routed hostname %q", got, hostB)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("creative backend did not receive a login")
	}
	select {
	case got := <-backendA.hosts:
		t.Fatalf("survival backend unexpectedly received host %q", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestGateBungeeGuardLoginUsesPerServerToken(t *testing.T) {
	st := testutil.NewTestStore(t)
	backend := startLoginCaptureBackend(t)
	host := "bungeeguard.example.test"
	mode := "bungeeguard"
	server, err := st.CreateServer(context.Background(), store.ServerInput{Name: "guarded", Upstream: backend.addr, PublicHost: &host, ForwardingMode: &mode})
	if err != nil {
		t.Fatalf("create guarded server: %v", err)
	}

	gateAddr := freeTCPAddress(t)
	rt, err := New(context.Background(), Options{
		Bind:       gateAddr,
		OnlineMode: false,
		Store:      st,
		Config:     config.Config{GateForwardingMode: "legacy"},
	})
	if err != nil {
		t.Fatalf("new BungeeGuard runtime: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan error, 1)
	go func() { started <- rt.Start(ctx) }()
	waitGateReady(t, rt)
	if registered := rt.proxy.Server("guarded"); registered == nil {
		t.Fatal("BungeeGuard backend was not registered after Gate startup")
	} else if _, ok := registered.ServerInfo().(*forwardingServerInfo); !ok {
		t.Fatalf("per-server BungeeGuard wrapper was replaced at startup: %T", registered.ServerInfo())
	}
	t.Cleanup(func() {
		for _, player := range rt.proxy.Players() {
			player.Disconnect(&component.Text{Content: "test cleanup"})
		}
		waitGatePlayersGone(t, rt.proxy)
		rt.proxy.Shutdown(&component.Text{Content: "test shutdown"})
		cancel()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Error("Gate runtime did not stop")
		}
	})

	loginThroughGate(t, gateAddr, host)
	select {
	case forwarded := <-backend.hosts:
		parts := strings.Split(forwarded, "\x00")
		if len(parts) != 4 || parts[0] != backend.addr {
			t.Fatalf("unexpected BungeeGuard forwarding address: %q", forwarded)
		}
		var properties []profile.Property
		if err := json.Unmarshal([]byte(parts[3]), &properties); err != nil {
			t.Fatalf("decode forwarded profile properties: %v", err)
		}
		if len(properties) != 1 || properties[0].Name != "bungeeguard-token" || properties[0].Value != server.BungeeGuardAllowedToken {
			t.Fatalf("backend received token properties %#v, want per-server token %q", properties, server.BungeeGuardAllowedToken)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("BungeeGuard backend did not receive a login")
	}
}

type loginCaptureBackend struct {
	listener net.Listener
	addr     string
	hosts    chan string
}

func startLoginCaptureBackend(t *testing.T) *loginCaptureBackend {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen backend: %v", err)
	}
	b := &loginCaptureBackend{listener: listener, addr: listener.Addr().String(), hosts: make(chan string, 1)}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		handshake, readErr := readMinecraftFrame(conn)
		if readErr != nil {
			return
		}
		host, parseErr := parseMinecraftHandshake(handshake)
		if parseErr != nil {
			return
		}
		b.hosts <- host
		if _, err := readMinecraftFrame(conn); err != nil { // Login Start
			return
		}
		var success bytes.Buffer
		writeVarInt(&success, 0x02)
		success.Write(make([]byte, 16))
		writeString(&success, "route_player")
		writeVarInt(&success, 0)
		_ = writeMinecraftFrame(conn, success.Bytes())
		// Keep the backend connection alive while the client-side test closes
		// first. This avoids Gate treating the backend close as a failed server
		// connection and starting its fallback-server flow during cleanup.
		time.Sleep(3 * time.Second)
	}()
	return b
}

func loginThroughGate(t *testing.T, gateAddr, host string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", gateAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial Gate: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	defer conn.Close()
	var handshake bytes.Buffer
	writeVarInt(&handshake, 0)
	writeVarInt(&handshake, 763)
	writeString(&handshake, host)
	_ = binary.Write(&handshake, binary.BigEndian, uint16(25565))
	writeVarInt(&handshake, 2)
	if err := writeMinecraftFrame(conn, handshake.Bytes()); err != nil {
		t.Fatalf("write client handshake: %v", err)
	}
	var login bytes.Buffer
	writeVarInt(&login, 0)
	writeString(&login, "route_player")
	login.WriteByte(1)
	login.Write(make([]byte, 16))
	if err := writeMinecraftFrame(conn, login.Bytes()); err != nil {
		t.Fatalf("write client login start: %v", err)
	}
	response, err := readMinecraftFrame(conn)
	if err != nil {
		t.Fatalf("read client login response: %v", err)
	}
	compressionEnabled := false
	for attempt := 0; attempt < 8; attempt++ {
		payload, decodeErr := decodeLoginFrame(response, compressionEnabled)
		if decodeErr != nil {
			t.Fatalf("decode client login packet: %v", decodeErr)
		}
		packetID, n, ok := readVarIntAt(payload, 0)
		if !ok {
			t.Fatal("client login packet has no packet id")
		}
		if packetID == 0x03 && !compressionEnabled { // Set Compression
			if _, _, ok := readVarIntAt(payload, n); !ok {
				t.Fatal("Set Compression packet has no threshold")
			}
			compressionEnabled = true
		} else if packetID == 0x02 {
			// Close the client side immediately after LoginSuccess so Gate can
			// finish the player teardown before the test shuts the proxy down.
			_ = conn.Close()
			return
		} else if packetID == 0x00 {
			t.Fatalf("client received Login Disconnect instead of LoginSuccess")
		}
		response, err = readMinecraftFrame(conn)
		if err != nil {
			t.Fatalf("read next client login packet: %v", err)
		}
	}
	t.Fatal("client did not receive LoginSuccess")
}

func waitGatePlayersGone(t *testing.T, p *javaproxy.Proxy) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p.PlayerCount() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Gate still has %d connected players", p.PlayerCount())
}

func decodeLoginFrame(frame []byte, compressionEnabled bool) ([]byte, error) {
	if !compressionEnabled {
		return frame, nil
	}
	dataLength, width, ok := readVarInt(frame)
	if !ok || width > len(frame) {
		return nil, fmt.Errorf("invalid compressed packet length")
	}
	data := frame[width:]
	if dataLength == 0 {
		return data, nil
	}
	reader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	decompressed, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return decompressed, nil
}

func freeTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve Gate address: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

func waitGateReady(t *testing.T, rt *Runtime) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if rt.Ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Gate runtime did not become ready")
}

func writeMinecraftFrame(w io.Writer, payload []byte) error {
	frame := append(encodeVarInt(len(payload)), payload...)
	_, err := w.Write(frame)
	return err
}

func readMinecraftFrame(r io.Reader) ([]byte, error) {
	lengthBytes := make([]byte, 0, 5)
	for len(lengthBytes) < 5 {
		one := []byte{0}
		if _, err := io.ReadFull(r, one); err != nil {
			return nil, err
		}
		lengthBytes = append(lengthBytes, one[0])
		if one[0]&0x80 == 0 {
			break
		}
	}
	length, _, ok := readVarInt(lengthBytes)
	if !ok || length < 0 {
		return nil, fmt.Errorf("invalid Minecraft frame length")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func parseMinecraftHandshake(payload []byte) (string, error) {
	position := 0
	if _, n, ok := readVarIntAt(payload, position); !ok {
		return "", fmt.Errorf("missing packet id")
	} else {
		position += n
	}
	if _, n, ok := readVarIntAt(payload, position); !ok {
		return "", fmt.Errorf("missing protocol")
	} else {
		position += n
	}
	hostLength, n, ok := readVarIntAt(payload, position)
	if !ok || hostLength < 0 || position+n+hostLength > len(payload) {
		return "", fmt.Errorf("invalid host")
	}
	return string(payload[position+n : position+n+hostLength]), nil
}

func writeVarInt(w io.Writer, value int) {
	_, _ = w.Write(encodeVarInt(value))
}

func writeString(w io.Writer, value string) {
	writeVarInt(w, len(value))
	_, _ = io.WriteString(w, value)
}

func TestIdleRuntimeAcceptsBackendAddedLater(t *testing.T) {
	st := testutil.NewTestStore(t)

	rt, err := New(context.Background(), Options{Bind: "0.0.0.0:25565", OnlineMode: false, Store: st})
	if err != nil {
		t.Fatalf("expected an idle runtime without backends, got %v", err)
	}
	if err := rt.syncServers(context.Background()); err != nil {
		t.Fatalf("sync empty server set: %v", err)
	}
	if got := rt.pickInitialServer(""); got != nil {
		t.Fatalf("expected no initial server while all backends are disabled, got %#v", got)
	}
	if _, err := st.CreateServer(context.Background(), store.ServerInput{Name: "late-lobby", Upstream: "127.0.0.1:25569"}); err != nil {
		t.Fatalf("create backend after runtime start: %v", err)
	}
	if err := rt.syncServers(context.Background()); err != nil {
		t.Fatalf("sync newly enabled backend: %v", err)
	}
	if got := rt.pickInitialServer(""); got == nil || got.ServerInfo().Name() != "late-lobby" {
		t.Fatalf("expected newly enabled backend to be selected, got %#v", got)
	}
}

func TestNewAndSyncServers(t *testing.T) {
	st := testutil.NewTestStore(t)

	_, err := st.CreateServer(context.Background(), store.ServerInput{Name: "lobby", Upstream: "127.0.0.1:25566"})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	rt, err := New(context.Background(), Options{Bind: "0.0.0.0:25565", OnlineMode: false, Store: st})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	if err := rt.syncServers(context.Background()); err != nil {
		t.Fatalf("sync servers: %v", err)
	}

	if got := rt.pickInitialServer(""); got == nil || got.ServerInfo().Name() != "lobby" {
		t.Fatalf("expected initial server lobby, got %#v", got)
	}
}

func TestBungeeGuardRegistersPerServerTokenWithoutGlobalSecret(t *testing.T) {
	st := testutil.NewTestStore(t)
	mode := "bungeeguard"
	created, err := st.CreateServer(context.Background(), store.ServerInput{Name: "legacy", Upstream: "127.0.0.1:25568", ForwardingMode: &mode})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	rt, err := New(context.Background(), Options{
		Bind: "0.0.0.0:25565", OnlineMode: false, Store: st,
		Config: config.Config{GateForwardingMode: "legacy"},
	})
	if err != nil {
		t.Fatalf("new bungeeguard runtime without global secret: %v", err)
	}
	if err := rt.syncServers(context.Background()); err != nil {
		t.Fatalf("sync bungeeguard server: %v", err)
	}
	registered := rt.proxy.Server("legacy")
	wrapped, ok := registered.ServerInfo().(*forwardingServerInfo)
	if !ok || wrapped.mode != gatecfg.BungeeGuardForwardingMode || wrapped.token != created.BungeeGuardAllowedToken {
		t.Fatalf("registered server token = %#v, want %q", registered.ServerInfo(), created.BungeeGuardAllowedToken)
	}
}

func TestGlobalBungeeGuardDoesNotApplyToUnconfiguredServer(t *testing.T) {
	rt := &Runtime{forwardingMode: gatecfg.BungeeGuardForwardingMode}
	if got := rt.serverForwardingMode(store.ServerDTO{}); got != gatecfg.LegacyForwardingMode {
		t.Fatalf("unconfigured server mode = %q, want legacy", got)
	}
}

func TestHostnamePingUsesServerMOTD(t *testing.T) {
	st := testutil.NewTestStore(t)
	host := "survival.example.test"
	addr := "survival.example.test:25565"
	motd := "Survival network"
	if _, err := st.CreateServer(context.Background(), store.ServerInput{
		Name: "survival", Upstream: "127.0.0.1:25569", PublicHost: &host, PublicAddr: &addr, PingMOTD: &motd,
	}); err != nil {
		t.Fatalf("create server: %v", err)
	}
	rt, err := New(context.Background(), Options{Bind: "0.0.0.0:25565", OnlineMode: false, Store: st})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	if err := rt.syncServers(context.Background()); err != nil {
		t.Fatalf("sync servers: %v", err)
	}
	got := rt.resolvePing("survival.example.test")
	description, ok := got.Description.(*component.Text)
	if !ok || description.Content != motd {
		t.Fatalf("ping description = %#v, want %q", got.Description, motd)
	}
}

func TestHostnameRouteCarriesServerPolicyScope(t *testing.T) {
	st := testutil.NewTestStore(t)
	publicHost := "play.example.test"
	publicAddr := "play.example.test:25565"
	_, err := st.CreateServer(context.Background(), store.ServerInput{
		Name: "survival", PublicHost: &publicHost, PublicAddr: &publicAddr, Upstream: "127.0.0.1:25567",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	rt, err := New(context.Background(), Options{Bind: "0.0.0.0:25565", Store: st})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	if err := rt.syncServers(context.Background()); err != nil {
		t.Fatalf("sync servers: %v", err)
	}
	if got := rt.pickInitialServer("play.example.test"); got == nil || got.ServerInfo().Name() != "survival" {
		t.Fatalf("expected hostname route to select survival, got %#v", got)
	}
	target, ok := rt.routeTargetForAddr(&net.TCPAddr{IP: net.ParseIP("203.0.113.10"), Port: 25565})
	if ok {
		t.Fatalf("unexpected route for unrelated address: %#v", target)
	}
	target, ok = rt.routeTargetForAddr(fakeAddr("play.example.test:25565"))
	if !ok || target.name != "survival" || target.serverID <= 0 {
		t.Fatalf("expected scoped route target, got target=%#v ok=%v", target, ok)
	}
}

func TestHostnameRouteAppliesServerScopedPolicy(t *testing.T) {
	ctx := context.Background()
	st := testutil.NewTestStore(t)
	publicHost := "restricted.example.test"
	server, err := st.CreateServer(ctx, store.ServerInput{
		Name: "restricted", PublicHost: &publicHost, Upstream: "127.0.0.1:25568",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	deny := "203.0.113.7"
	if _, err := st.CreateRule(ctx, store.RuleInput{
		Kind: "ip-deny", Target: deny, ServerID: &server.ID,
	}); err != nil {
		t.Fatalf("create server rule: %v", err)
	}

	rt, err := New(ctx, Options{Bind: "0.0.0.0:25565", Store: st})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	if err := rt.syncServers(ctx); err != nil {
		t.Fatalf("sync servers: %v", err)
	}
	target, ok := rt.routeTargetForAddr(fakeAddr("restricted.example.test:25565"))
	if !ok {
		t.Fatal("expected restricted hostname route")
	}
	result, err := st.EvaluateAttempt(ctx, store.EvaluateAttemptInput{
		ServerID: &target.serverID, IP: deny, Nickname: "player", Record: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("evaluate scoped route policy: %v", err)
	}
	if result.Allowed || result.Reason != "rule.deny" {
		t.Fatalf("expected server-scoped deny, got %#v", result)
	}
}

func TestSyncServersKeepsExistingRoutesWhenNewUpstreamIsInvalid(t *testing.T) {
	ctx := context.Background()
	st := testutil.NewTestStore(t)
	if _, err := st.CreateServer(ctx, store.ServerInput{Name: "healthy", Upstream: "127.0.0.1:25570"}); err != nil {
		t.Fatalf("create healthy server: %v", err)
	}
	rt, err := New(ctx, Options{Bind: "0.0.0.0:25565", Store: st})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	if err := rt.syncServers(ctx); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	if _, err := st.CreateServer(ctx, store.ServerInput{Name: "broken", Upstream: "invalid.invalid:25565"}); err != nil {
		t.Fatalf("create broken server fixture: %v", err)
	}
	if err := rt.syncServers(ctx); err == nil {
		t.Fatal("expected invalid upstream to fail sync")
	}
	if got := rt.pickInitialServer(""); got == nil || got.ServerInfo().Name() != "healthy" {
		t.Fatalf("expected existing route to remain after failed sync, got %#v", got)
	}
}

type fakeAddr string

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return string(a) }
