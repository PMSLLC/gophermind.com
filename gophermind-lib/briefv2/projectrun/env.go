package projectrun

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/envcheck"
	"gophermind/gophermind-lib/briefv2/executor"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/planner"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/sandbox"
	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
	"gophermind/gophermind-lib/config"
	"gophermind/gophermind-lib/gitenv"
	"gophermind/gophermind-lib/version"
)

// VersionInfo is the build stamp of the running binary.
type VersionInfo struct{ Version, Commit, Date string }

// Env is everything /project touches outside its own process, so tests can
// replace each piece.
type Env struct {
	SettingsPath   func() (string, error)
	LoadSettings   func(path string) (*settings.Config, error)
	BuildProviders func(cfg *settings.Config, secret func(name string) (string, error)) (map[string]provider.Provider, error)
	Probe          func(ctx context.Context, cfg *settings.Config) (*settings.Config, []envcheck.ProbeResult)
	ConfigDir      func() (string, error)
	VaultPath      func() (string, error)
	OpenVault      func(path, passphrase string) (*vault.Vault, error)
	// Backends returns the run-state stores: blackboard.NewFS and ledger.NewFS
	// over planner.LookupRun. Nothing to open or close, no database.
	Backends         func() (blackboard.Blackboard, ledger.Ledger)
	Git              func(ctx context.Context, repo string, args ...string) (string, error)
	LookPath         func(file, path string) (string, bool)
	SandboxPreflight func(ctx context.Context) error
	Dial             func(ctx context.Context, addr string) error
	Getenv           func(string) string
	Now              func() time.Time
	Version          func() VersionInfo
	Executable       func() (string, error)
	RunExecutor      func(ctx context.Context, o executor.Options) (executor.Report, error)
	Rand             io.Reader
}

// DefaultEnv is the real environment.
func DefaultEnv() Env {
	return Env{
		SettingsPath: settings.Path,
		LoadSettings: settings.Load,
		BuildProviders: func(cfg *settings.Config, secret func(string) (string, error)) (map[string]provider.Provider, error) {
			return cfg.BuildProviders(providerClient(cfg), secret)
		},
		Probe: func(ctx context.Context, cfg *settings.Config) (*settings.Config, []envcheck.ProbeResult) {
			return envcheck.ResolveBaseURLs(ctx, cfg, providerClient(cfg), envcheck.DefaultProbeTimeout)
		},
		ConfigDir: config.Dir,
		VaultPath: vaultPath,
		OpenVault: func(path, pass string) (*vault.Vault, error) { return vault.Open(path, pass, vault.Options{}) },
		Backends: func() (blackboard.Blackboard, ledger.Ledger) {
			return blackboard.NewFS(runDirOf), ledger.NewFS(runDirOf)
		},
		Git:              gitRun,
		LookPath:         envcheck.LookInPath,
		SandboxPreflight: sandbox.Preflight,
		Dial: func(ctx context.Context, addr string) error {
			c, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", addr)
			if err != nil {
				return err
			}
			return c.Close()
		},
		Getenv:      os.Getenv,
		Now:         time.Now,
		Version:     func() VersionInfo { return VersionInfo{version.Version, version.Commit, version.Date} },
		Executable:  os.Executable,
		RunExecutor: executor.Run,
		Rand:        rand.Reader,
	}
}

// providerClient is the no-redirect, no-proxy client the brief commands use.
func providerClient(cfg *settings.Config) *http.Client {
	return provider.NewHTTPClient(cfg.MaxCallTimeout() + time.Minute)
}

func runDirOf(runID string) (string, error) {
	rec, err := planner.LookupRun(runID)
	if err != nil {
		return "", err
	}
	return rec.RunDir, nil
}

func vaultPath() (string, error) {
	if p := os.Getenv("GOPHERMIND_VAULT_PATH"); p != "" {
		return p, nil
	}
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "vault.age"), nil
}

// gitAllowed reports whether args is one of the few read-only git calls the
// preflight makes. A rev or branch name may not look like an option.
func gitAllowed(args []string) bool {
	eq := func(want ...string) bool {
		if len(args) != len(want) {
			return false
		}
		for i := range want {
			if args[i] != want[i] {
				return false
			}
		}
		return true
	}
	switch {
	case eq("rev-parse", "--is-inside-work-tree"), eq("rev-parse", "--show-toplevel"), eq("rev-parse", "HEAD"),
		eq("symbolic-ref", "--short", "HEAD"), eq("status", "--porcelain", "-uall"):
		return true
	case len(args) == 3 && args[0] == "rev-parse" && args[1] == "--verify":
		return strings.HasSuffix(args[2], "^{commit}") && !strings.HasPrefix(args[2], "-")
	case len(args) == 3 && args[0] == "branch" && args[1] == "--list":
		return args[2] != "" && !strings.HasPrefix(args[2], "-")
	}
	return false
}

func gitRun(ctx context.Context, repo string, args ...string) (string, error) {
	if !gitAllowed(args) {
		return "", errors.New("projectrun: git call not allowed in preflight")
	}
	full := append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "--no-optional-locks"}, args...)
	cmd := gitenv.CommandContext(ctx, repo, full...)
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}
