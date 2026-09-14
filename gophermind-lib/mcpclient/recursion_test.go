package mcpclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// `gophermind mcp` builds its tool registry the same way every other
// invocation does, so it also runs Load. Without a guard, configuring
// gophermind as its own stdio MCP server would have each child spawn another
// child forever — an unbounded fork bomb. A process stamped as an MCP child
// must load no servers at all.
func TestLoadIsInertInAnMCPChild(t *testing.T) {
	t.Setenv(ChildEnvVar, "1")

	path := writeServers(t, map[string]string{"local": stdioEntry(t)})
	m, err := Load(context.Background(), path, "", func(string) {
		t.Error("a child must not even try to connect")
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer m.Close()

	if got := len(m.Tools()); got != 0 {
		t.Errorf("an MCP child must load no servers, got %d tools", got)
	}
}

// The guard only works if children are actually stamped.
func TestStdioChildIsStamped(t *testing.T) {
	// A server that records its own environment, then exits.
	out := filepath.Join(t.TempDir(), "env.txt")
	cfg := ServerConfig{
		Name:      "probe",
		Transport: TransportStdio,
		Command:   "sh",
		Args:      []string{"-c", `printenv ` + ChildEnvVar + ` > "$PROBE_OUT"; sleep 0.2`},
		Env:       map[string]string{"PROBE_OUT": out},
	}
	tr, err := newStdioTransport(cfg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Let the child write before we tear it down.
	_, _ = tr.Send(context.Background(), []byte(`{"jsonrpc":"2.0","method":"noop"}`))
	deadline := 40
	for i := 0; i < deadline; i++ {
		if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
			tr.Close()
			if got := string(b); got != "1\n" {
				t.Errorf("child stamp = %q, want \"1\\n\"", got)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	tr.Close()
	t.Fatal("child never recorded its environment")
}
