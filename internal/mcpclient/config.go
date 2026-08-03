// Package mcpclient connects gophermind to third-party MCP servers and exposes
// their tools as ordinary gophermind tools.
//
// It is the outbound counterpart to internal/mcp, which serves gophermind's own
// tools over MCP and never dials out. The two packages share a protocol but no
// code, and neither imports the other.
package mcpclient

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// NameSeparator joins a server name and a remote tool name into the name
// gophermind registers ("pelagosnow" + "search" => "pelagosnow__search").
//
// Namespacing keeps a remote server from shadowing a builtin tool and keeps two
// servers from colliding with each other. safety.Gated keys off this separator
// to gate every MCP tool by default, so it must not appear inside a server or
// tool name — LoadConfigs and adaptTools both reject names containing it.
const NameSeparator = "__"

// Transport kinds.
const (
	TransportHTTP  = "http"
	TransportStdio = "stdio"
)

// ServerConfig describes one MCP server to connect to. It is the shape of both
// an entry in config.json's "mcpServers" object and a per-project
// .gophermind/<name>.mcp.json file.
type ServerConfig struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"` // "http" | "stdio"
	URL       string            `json:"url"`       // http
	Headers   map[string]string `json:"headers"`   // http
	Command   string            `json:"command"`   // stdio
	Args      []string          `json:"args"`      // stdio
	Env       map[string]string `json:"env"`       // stdio
	Disabled  bool              `json:"disabled"`
}

// envRef matches a ${VAR} reference in a configured string.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces every ${VAR} in s with that variable's value.
//
// An unset variable is an error rather than an empty string: a silently blank
// Authorization header produces a 401 far from its cause, and a blank
// connection string fails even less legibly. Keeping references unresolved-or-
// fatal is what lets tokens stay in the environment while the config file
// itself remains safe to commit.
func expandEnv(s string) (string, error) {
	var missing []string
	out := envRef.ReplaceAllStringFunc(s, func(ref string) string {
		name := envRef.FindStringSubmatch(ref)[1]
		val, ok := os.LookupEnv(name)
		if !ok {
			missing = append(missing, name)
			return ""
		}
		return val
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("undefined environment variable %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// expand resolves ${VAR} references across every configured string.
func (s *ServerConfig) expand() error {
	var err error
	if s.URL, err = expandEnv(s.URL); err != nil {
		return fmt.Errorf("url: %w", err)
	}
	if s.Command, err = expandEnv(s.Command); err != nil {
		return fmt.Errorf("command: %w", err)
	}
	for i, a := range s.Args {
		if s.Args[i], err = expandEnv(a); err != nil {
			return fmt.Errorf("args[%d]: %w", i, err)
		}
	}
	for k, v := range s.Headers {
		if s.Headers[k], err = expandEnv(v); err != nil {
			return fmt.Errorf("headers[%q]: %w", k, err)
		}
	}
	for k, v := range s.Env {
		if s.Env[k], err = expandEnv(v); err != nil {
			return fmt.Errorf("env[%q]: %w", k, err)
		}
	}
	return nil
}

// validate checks the fields required by the declared transport.
func (s *ServerConfig) validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.Contains(s.Name, NameSeparator) {
		return fmt.Errorf("server name %q must not contain %q", s.Name, NameSeparator)
	}
	switch s.Transport {
	case TransportHTTP:
		if strings.TrimSpace(s.URL) == "" {
			return fmt.Errorf("transport %q requires a url", s.Transport)
		}
	case TransportStdio:
		if strings.TrimSpace(s.Command) == "" {
			return fmt.Errorf("transport %q requires a command", s.Transport)
		}
	case "":
		return fmt.Errorf("transport is required (%q or %q)", TransportHTTP, TransportStdio)
	default:
		return fmt.Errorf("unknown transport %q (want %q or %q)", s.Transport, TransportHTTP, TransportStdio)
	}
	return nil
}

// LoadConfigs reads MCP server definitions from the global config file and the
// per-project directory, merging them by name.
//
// globalPath is a config.json whose optional "mcpServers" object maps a server
// name to its definition. projectDir is scanned for *.mcp.json files, each
// holding a single definition. A project entry replaces a global one with the
// same name, matching how a working-directory .env outranks the global config.
//
// Missing files and a missing directory are not errors — MCP is opt-in. A
// malformed definition, an unknown transport, or an unset ${VAR} is fatal, so a
// typo is reported at startup rather than as a puzzling failure mid-session.
// Disabled servers are dropped after validation, so disabling one does not hide
// a mistake in it.
func LoadConfigs(globalPath, projectDir string) ([]ServerConfig, error) {
	byName := map[string]ServerConfig{}

	global, err := readGlobal(globalPath)
	if err != nil {
		return nil, err
	}
	for _, s := range global {
		byName[s.Name] = s
	}

	project, err := readProjectDir(projectDir)
	if err != nil {
		return nil, err
	}
	for _, s := range project {
		byName[s.Name] = s
	}

	out := make([]ServerConfig, 0, len(byName))
	for _, s := range byName {
		if err := s.validate(); err != nil {
			return nil, fmt.Errorf("mcp server %q: %w", s.Name, err)
		}
		if err := s.expand(); err != nil {
			return nil, fmt.Errorf("mcp server %q: %w", s.Name, err)
		}
		if s.Disabled {
			continue
		}
		out = append(out, s)
	}
	// Stable order so startup logs and tool ordering do not vary run to run.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// readGlobal extracts the "mcpServers" object from a config.json.
func readGlobal(path string) ([]ServerConfig, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var doc struct {
		Servers map[string]ServerConfig `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make([]ServerConfig, 0, len(doc.Servers))
	for name, s := range doc.Servers {
		if s.Name == "" {
			s.Name = name // the map key names the server
		}
		out = append(out, s)
	}
	return out, nil
}

// readProjectDir reads every *.mcp.json in dir as a single server definition.
func readProjectDir(dir string) ([]ServerConfig, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []ServerConfig
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mcp.json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var s ServerConfig
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if s.Name == "" {
			s.Name = strings.TrimSuffix(e.Name(), ".mcp.json") // the filename names the server
		}
		out = append(out, s)
	}
	return out, nil
}
