package mcpclient

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"gophermind/gophermind-lib/tools"
)

// connectTimeout bounds the handshake with a single server. Servers are dialed
// concurrently, so N unreachable servers cost one timeout in total rather than N.
const connectTimeout = 5 * time.Second

// ChildEnvVar marks a process started as an MCP server by this client.
//
// `gophermind mcp` serves gophermind's own tools, and it builds its registry —
// which means it runs Load — like any other invocation. Pointing gophermind at
// itself as a stdio server would therefore have each child spawn another child,
// without bound. Every stdio child is stamped with this variable and Load does
// nothing when it is set, so the recursion stops at the first level.
const ChildEnvVar = "GOPHERMIND_MCP_CHILD"

// Manager owns the live connections to every configured MCP server and the
// tools they contribute. Close it at shutdown to stop stdio subprocesses.
type Manager struct {
	clients []*Client
	tools   []tools.Tool
}

// Tools returns the adapted tools, ordered by name for a stable registry.
func (m *Manager) Tools() []tools.Tool {
	if m == nil {
		return nil
	}
	return m.tools
}

// Close shuts down every server connection.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	var first error
	for _, c := range m.clients {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Load reads the MCP configuration, connects to every enabled server, and
// returns their tools.
//
// A configuration problem is fatal: a typo should be reported at startup rather
// than surface as a puzzling absence later. A server that cannot be reached is
// only a warning, because one dead server must not stop a session — that is the
// same tolerance LoadPlugins shows a missing plugin directory.
//
// warn receives one message per unreachable server; pass nil to discard them.
func Load(ctx context.Context, globalPath, projectDir string, warn func(string)) (*Manager, error) {
	if warn == nil {
		warn = func(string) {}
	}
	// This process is itself an MCP server started by a parent gophermind.
	// Loading servers here is what would make the spawn recursive.
	if os.Getenv(ChildEnvVar) != "" {
		return &Manager{}, nil
	}
	configs, err := LoadConfigs(globalPath, projectDir)
	if err != nil {
		return nil, err
	}
	if len(configs) == 0 {
		return &Manager{}, nil
	}

	type result struct {
		client *Client
		tools  []tools.Tool
	}
	var (
		mu      sync.Mutex
		results = make([]*result, len(configs))
		wg      sync.WaitGroup
	)
	httpClient := &http.Client{Timeout: 60 * time.Second}

	for i, cfg := range configs {
		wg.Add(1)
		go func(i int, cfg ServerConfig) {
			defer wg.Done()
			c, ts, err := connect(ctx, cfg, httpClient)
			if err != nil {
				mu.Lock()
				warn(fmt.Sprintf("mcp server %q unavailable: %v", cfg.Name, err))
				mu.Unlock()
				return
			}
			results[i] = &result{client: c, tools: ts}
		}(i, cfg)
	}
	wg.Wait()

	m := &Manager{}
	for _, r := range results {
		if r == nil {
			continue
		}
		m.clients = append(m.clients, r.client)
		m.tools = append(m.tools, r.tools...)
	}
	sort.Slice(m.tools, func(i, j int) bool { return m.tools[i].Name < m.tools[j].Name })
	return m, nil
}

// connect dials one server, performs the handshake, and adapts its tools. Any
// failure closes the transport so a half-open connection is never leaked.
func connect(ctx context.Context, cfg ServerConfig, httpClient *http.Client) (*Client, []tools.Tool, error) {
	dialCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	var (
		tr  Transport
		err error
	)
	switch cfg.Transport {
	case TransportStdio:
		tr, err = newStdioTransport(cfg)
	case TransportHTTP:
		tr = newHTTPTransport(cfg, httpClient)
	default:
		return nil, nil, fmt.Errorf("unknown transport %q", cfg.Transport)
	}
	if err != nil {
		return nil, nil, err
	}

	c := NewClient(tr)
	if err := c.Initialize(dialCtx); err != nil {
		c.Close()
		return nil, nil, err
	}
	descs, err := c.ListTools(dialCtx)
	if err != nil {
		c.Close()
		return nil, nil, err
	}
	ts, err := adaptTools(c, cfg.Name, descs)
	if err != nil {
		c.Close()
		return nil, nil, err
	}
	return c, ts, nil
}
