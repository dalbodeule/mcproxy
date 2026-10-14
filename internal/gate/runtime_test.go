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
	"go.minekube.com/gate/pkg/util/netutil"
	gateuuid "go.minekube.com/gate/pkg/util/uuid"
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
	if raceBuild {
		t.Skip("Gate v0.74.41 has an upstream shutdown race in this network integration path")
	}
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
		// Gate v0.74.41 can deadlock when a test synchronously calls
		// player.Disconnect while a backend transition is closing. Cancelling
		// the runtime context lets Gate close its listener and unwind all
		// session goroutines without entering that lock cycle.
		cancel()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Log("Gate runtime shutdown is still unwinding after test cancellation")
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
	if raceBuild {
		t.Skip("Gate v0.74.41 has an upstream shutdown race in this network integration path")
	}
	for _, globalMode := range []string{"legacy"} {
		t.Run(globalMode, func(t *testing.T) {
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
				Config:     config.Config{GateForwardingMode: globalMode},
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
			} else if globalMode == "legacy" {
				if _, ok := registered.ServerInfo().(*forwardingServerInfo); !ok {
					t.Fatalf("per-server BungeeGuard wrapper was replaced at startup: %T", registered.ServerInfo())
				}
			}
			t.Cleanup(func() {
				cancel()
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Log("Gate runtime shutdown is still unwinding after test cancellation")
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
		})
	}
}

func TestGlobalBungeeGuardModeIsRejected(t *testing.T) {
	st := testutil.NewTestStore(t)
	if _, err := st.CreateServer(context.Background(), store.ServerInput{Name: "guarded", Upstream: "127.0.0.1:25568"}); err != nil {
		t.Fatalf("create server: %v", err)
	}
	_, err := New(context.Background(), Options{
		Bind: "0.0.0.0:25565", OnlineMode: false, Store: st,
		Config: config.Config{GateForwardingMode: "bungeeguard"},
	})
	if err == nil || !strings.Contains(err.Error(), "must be configured per server") {
		t.Fatalf("global BungeeGuard mode error = %v", err)
	}
}

type loginCaptureBackend struct {
	listener net.Listener
	addr     string
	hosts    chan string
	done     chan struct{}
}

func startLoginCaptureBackend(t *testing.T) *loginCaptureBackend {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen backend: %v", err)
	}
	b := &loginCaptureBackend{listener: listener, addr: listener.Addr().String(), hosts: make(chan string, 1), done: make(chan struct{})}
	t.Cleanup(func() {
		close(b.done)
		_ = listener.Close()
	})
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
		// This fixture only verifies that Gate selected the expected backend and
		// rewrote its forwarding handshake. Deliberately stop before LoginSuccess;
		// completing a full transition and closing the synthetic client triggers
		// an upstream Gate v0.74.41 shutdown lock cycle.
		<-b.done
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
	var handshake bytes.Buffer
	writeVarInt(&handshake, 0)
	writeVarInt(&handshake, 777) // Minecraft 26.3
	writeString(&handshake, host)
	_ = binary.Write(&handshake, binary.BigEndian, uint16(25565))
	writeVarInt(&handshake, 2)
	if err := writeMinecraftFrame(conn, handshake.Bytes()); err != nil {
		t.Fatalf("write client handshake: %v", err)
	}
	var login bytes.Buffer
	writeVarInt(&login, 0)
	writeString(&login, "route_player")
	// Protocol 1.20.2+ (including 26.3/777) encodes the holder UUID directly;
	// the preceding boolean belongs to older Login Start layouts.
	login.Write(make([]byte, 16))
	if err := writeMinecraftFrame(conn, login.Bytes()); err != nil {
		t.Fatalf("write client login start: %v", err)
	}
	// Protocol 777 requires the client to acknowledge Login Success before Gate
	// fires PostLogin and begins the initial backend transition. Older fixtures
	// stopped after Login Start, which silently tested only pre-26.3 behavior.
	compressionThreshold := -1
	for attempt := 0; attempt < 8; attempt++ {
		raw, readErr := readMinecraftFrame(conn)
		if readErr != nil {
			t.Fatalf("read 26.3 Login Success: %v", readErr)
		}
		success := raw
		if compressionThreshold >= 0 {
			var decodeErr error
			success, decodeErr = decodeCompressedMinecraftFrame(raw)
			if decodeErr != nil {
				t.Fatalf("decode compressed 26.3 login packet: %v", decodeErr)
			}
		}
		packetID, packetPrefix, ok := readVarIntAt(success, 0)
		if !ok {
			t.Fatal("invalid clientbound login packet")
		}
		if packetID == 2 {
			break
		}
		// Gate may send Set Compression (packet 3) before Login Success.
		if packetID == 3 {
			threshold, _, thresholdOK := readVarIntAt(success, packetPrefix)
			if !thresholdOK {
				t.Fatal("invalid Set Compression threshold")
			}
			compressionThreshold = threshold
			continue
		}
		if attempt == 7 {
			t.Fatalf("expected 26.3 Login Success packet 2, got %d", packetID)
		}
	}
	ack := encodeVarInt(3) // Login Acknowledged
	if compressionThreshold >= 0 {
		ack = append(encodeVarInt(0), ack...)
	}
	if err := writeMinecraftFrame(conn, ack); err != nil {
		t.Fatalf("write Login Acknowledged: %v", err)
	}
	// The route assertion is made by the capture backend after it receives the
	// handshake and Login Start. Keep the client connection open until the test
	// cleanup so Gate can shut down without a synthetic client close race.
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

func decodeCompressedMinecraftFrame(raw []byte) ([]byte, error) {
	dataLength, prefix, ok := readVarIntAt(raw, 0)
	if !ok || dataLength < 0 {
		return nil, fmt.Errorf("invalid compressed data length")
	}
	if dataLength == 0 {
		return raw[prefix:], nil
	}
	reader, err := zlib.NewReader(bytes.NewReader(raw[prefix:]))
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
	if len(decompressed) != dataLength {
		return nil, fmt.Errorf("compressed data length mismatch: declared=%d actual=%d", dataLength, len(decompressed))
	}
	return decompressed, nil
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

func TestConcurrentLoginReservationIsPerIPAndReleased(t *testing.T) {
	rt := &Runtime{
		maxLoginsPerIP: 1,
		pendingLogins:  make(map[gateuuid.UUID]pendingLogin),
		pendingByIP:    make(map[string]int),
	}
	first := gateuuid.New()
	second := gateuuid.New()
	if !rt.reserveLogin("198.51.100.10", first, true) {
		t.Fatal("first login reservation was rejected")
	}
	if rt.reserveLogin("198.51.100.10", second, true) {
		t.Fatal("second login from the same IP bypassed the limit")
	}
	if !rt.reserveLogin("198.51.100.11", second, true) {
		t.Fatal("different IP was incorrectly limited")
	}
	rt.releaseLogin(first)
	if !rt.reserveLogin("198.51.100.10", gateuuid.New(), true) {
		t.Fatal("released login slot was not reusable")
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

	rotated, err := st.RotateBungeeGuardToken(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("rotate BungeeGuard token: %v", err)
	}
	if rotated.BungeeGuardAllowedToken == created.BungeeGuardAllowedToken {
		t.Fatal("rotated BungeeGuard token did not change")
	}
	if err := rt.syncServers(context.Background()); err != nil {
		t.Fatalf("sync rotated BungeeGuard token: %v", err)
	}
	registered = rt.proxy.Server("legacy")
	wrapped, ok = registered.ServerInfo().(*forwardingServerInfo)
	if !ok || wrapped.token != rotated.BungeeGuardAllowedToken {
		t.Fatalf("registered rotated token = %#v, want %q", registered.ServerInfo(), rotated.BungeeGuardAllowedToken)
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
