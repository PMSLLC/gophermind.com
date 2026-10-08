package projectrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/settings"
	"gophermind/gophermind-lib/briefv2/vault"
)

func TestPreflightToolsAndSettingsNotCreated(t *testing.T) {
	t.Run("missing settings file", func(t *testing.T) {
		r := newRig(t)
		path := filepath.Join(r.cfgDir, "gophermind.yaml")
		os.Remove(path)
		got := wantFail(t, r.run(), "settings")
		if !strings.Contains(got.Detail, "gophermind.yaml not found at "+path) || !strings.Contains(got.Fix, "project-runbook.md") {
			t.Errorf("%+v", got)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("the preflight created the settings file: %v", err)
		}
	})
	t.Run("unknown planner key is refused", func(t *testing.T) {
		r := newRig(t)
		path := filepath.Join(r.cfgDir, "gophermind.yaml")
		b, _ := os.ReadFile(path)
		body := strings.Replace(string(b), "clarify_max_calls", "clarify_max_call", 1)
		os.WriteFile(path, []byte(body), 0o600)
		wantFail(t, r.run(), "settings")
	})
	t.Run("go not on the toolchain path", func(t *testing.T) {
		r := newRig(t)
		r.cfg.Toolchain = map[string]string{"PATH": t.TempDir()}
		r.writeSettings()
		got := wantFail(t, r.run(), "go toolchain")
		if !strings.HasPrefix(got.Fix, "add ") || !strings.Contains(got.Fix, "to toolchain.PATH in "+filepath.Join(r.cfgDir, "gophermind.yaml")) {
			t.Errorf("fix %q", got.Fix)
		}
	})
	t.Run("git not on the process path", func(t *testing.T) {
		r := newRig(t)
		r.environ["PATH"] = t.TempDir()
		wantFail(t, r.run(), "git")
	})
	t.Run("sandbox refused", func(t *testing.T) {
		r := newRig(t)
		r.cfg.Executor.Sandbox = "on"
		r.writeSettings()
		r.env.SandboxPreflight = func(context.Context) error { return errors.New("no sandbox-exec") }
		wantFail(t, r.run(), "sandbox")
		r.cfg.Executor.Sandbox = "off"
		r.writeSettings()
		wantOK(t, r.run(), "sandbox")
	})
	t.Run("sandbox available on darwin", func(t *testing.T) {
		if runtime.GOOS != "darwin" {
			t.Skip("sandbox is on by default only on darwin")
		}
		r := newRig(t)
		r.cfg.Executor.Sandbox = "on"
		r.writeSettings()
		wantOK(t, r.run(), "sandbox")
	})
	t.Run("disk space", func(t *testing.T) {
		r := newRig(t)
		old := minFreeBytes
		minFreeBytes = 1 << 62
		defer func() { minFreeBytes = old }()
		wantFail(t, r.run(), "disk space")
	})
}

func TestPreflightRequirePrivate(t *testing.T) {
	two := func(r *rig, mode string, publicInAny bool) {
		r.cfg.Providers = append(r.cfg.Providers, settings.ProviderConfig{
			Name: "pub", BaseURL: "https://example.com/v1", Visibility: settings.Public, MaxConcurrent: 1,
			Models: []settings.ModelEntry{{ID: "m", ContextTokens: 1000}},
		})
		if publicInAny {
			r.cfg.Models["any"] = []string{"fake/fixture", "pub/m"}
		}
		r.cfg.Privacy.Mode = mode
		r.writeSettings()
		r.o.RequirePrivate = true
	}
	t.Run("public entry fails", func(t *testing.T) {
		r := newRig(t)
		two(r, "need_to_know", true)
		got := wantFail(t, r.run(), "privacy")
		if !strings.Contains(got.Detail, "pub") {
			t.Errorf("detail %q does not name the provider", got.Detail)
		}
	})
	t.Run("need_to_know fails", func(t *testing.T) {
		r := newRig(t)
		r.cfg.Privacy.Mode = "need_to_know"
		r.writeSettings()
		r.o.RequirePrivate = true
		wantFail(t, r.run(), "privacy")
	})
	t.Run("all private passes", func(t *testing.T) {
		r := newRig(t)
		r.cfg.Privacy.Mode = "private_only"
		r.writeSettings()
		r.o.RequirePrivate = true
		wantOK(t, r.run(), "privacy")
	})
	t.Run("not required has no check", func(t *testing.T) {
		r := newRig(t)
		if _, ok := find(r.run(), "privacy"); ok {
			t.Error("privacy check without --require-private")
		}
	})
}

func TestPreflightProvidersFallback(t *testing.T) {
	t.Run("fallback answers", func(t *testing.T) {
		r := newRig(t)
		dead := "http://" + closedAddr(t) + "/v1"
		r.cfg.Providers[0].BaseURL = dead
		r.cfg.Providers[0].BaseURLFallbacks = []string{r.srv.URL + "/v1"}
		r.writeSettings()
		res := r.run()
		got := wantOK(t, res, "provider fake")
		if !strings.Contains(got.Detail, "answered on fallback host") {
			t.Errorf("detail %q", got.Detail)
		}
		if res.Config == nil || res.Config.Providers[0].BaseURL != r.srv.URL+"/v1" {
			t.Errorf("config does not carry the fallback: %+v", res.Config)
		}
		if len(res.Probes) != 1 || !res.Probes[0].Fallback {
			t.Errorf("probes %+v", res.Probes)
		}
	})
	t.Run("both closed fails naming the provider", func(t *testing.T) {
		r := newRig(t)
		r.cfg.Providers[0].BaseURL = "http://" + closedAddr(t) + "/v1"
		r.cfg.Providers[0].BaseURLFallbacks = []string{"http://" + closedAddr(t) + "/v1"}
		r.writeSettings()
		got := wantFail(t, r.run(), "provider fake")
		if got.Detail != "no base_url answered" || !strings.Contains(got.Fix, "fake") || !strings.Contains(got.Fix, "base_url_fallbacks") {
			t.Errorf("%+v", got)
		}
	})
	t.Run("a provider only the any chain names is probed", func(t *testing.T) {
		r := newRig(t)
		r.cfg.Providers = append(r.cfg.Providers, settings.ProviderConfig{
			Name: "spare", BaseURL: "http://" + closedAddr(t) + "/v1", Visibility: settings.Private, MaxConcurrent: 1,
			Models: []settings.ModelEntry{{ID: "m", ContextTokens: 1000}},
		})
		r.cfg.Models["any"] = []string{"fake/fixture", "spare/m"}
		r.writeSettings()
		res := r.run()
		wantOK(t, res, "provider fake")
		wantFail(t, res, "provider spare")
	})
	t.Run("missing provider key surfaces the build error", func(t *testing.T) {
		r := newRig(t)
		r.cfg.Providers[0].APIKeySecret = "FAKE_API_KEY"
		r.writeSettings()
		res := r.run()
		got := wantFail(t, res, "provider fake key")
		if !strings.Contains(got.Detail, "FAKE_API_KEY") {
			t.Errorf("detail %q", got.Detail)
		}
		if _, ok := find(res, "vault"); !ok {
			t.Error("a provider api_key_secret needs the vault")
		}
	})
	t.Run("provider key present", func(t *testing.T) {
		r := newRig(t)
		r.cfg.Providers[0].APIKeySecret = "FAKE_API_KEY"
		r.writeSettings()
		r.seed(vault.HarnessScope, map[string]string{"FAKE_API_KEY": "CANARY-key-value"})
		res := r.run()
		if f := Failed(res.Checks); len(f) != 0 {
			t.Fatalf("%+v", f)
		}
		if res.Store == nil {
			t.Error("the opened vault is not returned")
		}
	})
}

func TestPreflightExpectBinaryCommit(t *testing.T) {
	r := newRig(t)
	r.o.ExpectBinaryCommit = "abc12"
	got := wantOK(t, r.run(), "binary")
	if !strings.Contains(got.Detail, "v0.0.0-test") || !strings.Contains(got.Detail, "abc1234def") {
		t.Errorf("detail %q", got.Detail)
	}
	r.o.ExpectBinaryCommit = "ffff"
	got = wantFail(t, r.run(), "binary")
	if !strings.Contains(got.Fix, "scripts/build-dev-binary.sh") {
		t.Errorf("fix %q", got.Fix)
	}
	r.env.Version = func() VersionInfo { return VersionInfo{"dev", "none", ""} }
	r.o.ExpectBinaryCommit = "none"
	wantFail(t, r.run(), "binary")
	r.o.ExpectBinaryCommit = ""
	if _, ok := find(r.run(), "binary"); ok {
		t.Error("binary check without --expect-binary-commit")
	}
}
