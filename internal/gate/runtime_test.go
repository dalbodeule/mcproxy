package gate

import (
	"context"
	"testing"

	"mcproxy/internal/store"
	"mcproxy/internal/testutil"
)

func TestNewFailsWithoutBackendServers(t *testing.T) {
	st := testutil.NewTestStore(t)

	_, err := New(context.Background(), Options{Bind: "0.0.0.0:25565", OnlineMode: false, Store: st})
	if err == nil {
		t.Fatalf("expected error when no backend servers are registered")
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
