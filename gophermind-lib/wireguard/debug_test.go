package wireguard

import (
	"context"
	"fmt"
	"net/netip"
	"testing"
	"time"
)

func TestDebugIPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	srv, err := NewServer(ctx, ServerConfig{
		ListenPort: freePort(t),
		Address:    netip.MustParseAddr("10.66.0.1"),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	srv.mu.Lock()
	out, err := srv.dev.IpcGet()
	fmt.Printf("IpcGet err=%v out=%q\n", err, out)
	srv.mu.Unlock()

	t.Logf("PublicKey: %q", srv.PublicKey())
}
