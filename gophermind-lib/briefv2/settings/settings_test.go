package settings_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/settings"
)

func TestDefaultIsValid(t *testing.T) {
	if err := settings.Default().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestFirstLoadWritesDefaultsWithPrivateModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "conf")
	path := filepath.Join(dir, "gophermind.yaml")
	c, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, settings.Default()) {
		t.Errorf("first load did not return the defaults:\n%+v", c)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", fi, err)
	}
	if di, _ := os.Stat(dir); di.Mode().Perm() != 0o700 {
		t.Errorf("directory mode = %v, want 0700", di.Mode().Perm())
	}
	// The written file loads back to the same values, durations included.
	again, err := settings.Load(path)
	if err != nil || !reflect.DeepEqual(again, settings.Default()) {
		t.Errorf("round trip differs: %v\n%+v", err, again)
	}
	if again.Defaults.CallTimeout != 10*time.Minute {
		t.Errorf("call_timeout = %v", again.Defaults.CallTimeout)
	}
}

func TestLoadReadsAnExistingFileWithoutRewritingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gophermind.yaml")
	if _, err := settings.Load(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	edited := strings.Replace(string(b), "max_revisions: 2", "max_revisions: 5", 1)
	if edited == string(b) {
		t.Fatal("the default file no longer contains max_revisions: 2; update this test")
	}
	os.WriteFile(path, []byte(edited), 0o600)
	c, err := settings.Load(path)
	if err != nil || c.Defaults.MaxRevisions != 5 {
		t.Fatalf("Load = %+v, %v", c, err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != edited {
		t.Error("Load rewrote an existing file")
	}
}

func TestPathHonorsTheConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)
	p, err := settings.Path()
	if err != nil || p != filepath.Join(dir, "gophermind.yaml") {
		t.Errorf("Path = %q, %v", p, err)
	}
}

func TestLoadRefusesAKeyValueInTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gophermind.yaml")
	if _, err := settings.Load(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	withKey := strings.Replace(string(b), "name: kilo\n", "name: kilo\n    api_key: sk-LEAK-4417\n", 1)
	os.WriteFile(path, []byte(withKey), 0o600)
	_, err := settings.Load(path)
	if err == nil {
		t.Fatal("a file with api_key was accepted")
	}
	if strings.Contains(err.Error(), "sk-LEAK-4417") {
		t.Errorf("the error repeats the key: %v", err)
	}
}

func TestLoadRejectsBadDurationsAndBadYAML(t *testing.T) {
	for name, edit := range map[string]func(string) string{
		"unparsable duration": func(s string) string {
			return strings.Replace(s, "call_timeout: 10m0s", "call_timeout: ten minutes", 1)
		},
		"not yaml": func(string) string { return "providers: [unclosed" },
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gophermind.yaml")
			settings.Load(path)
			b, _ := os.ReadFile(path)
			os.WriteFile(path, []byte(edit(string(b))), 0o600)
			if _, err := settings.Load(path); err == nil {
				t.Error("Load accepted a broken file")
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*settings.Config)
		want   string
	}{
		{"no providers", func(c *settings.Config) { c.Providers = nil }, "at least one provider"},
		{"duplicate provider", func(c *settings.Config) { c.Providers[1].Name = "mini" }, "duplicate provider"},
		{"slash in name", func(c *settings.Config) { c.Providers[0].Name = "a/b" }, "slash"},
		{"bad visibility", func(c *settings.Config) { c.Providers[0].Visibility = "shared" }, "visibility"},
		{"no base url", func(c *settings.Config) { c.Providers[0].BaseURL = "" }, "base_url"},
		{"zero concurrency", func(c *settings.Config) { c.Providers[0].MaxConcurrent = 0 }, "max_concurrent"},
		{"no models", func(c *settings.Config) { c.Providers[0].Models = nil }, "at least one model"},
		{"zero context", func(c *settings.Config) { c.Providers[0].Models[0].ContextTokens = 0 }, "context_tokens"},
		{"duplicate model", func(c *settings.Config) {
			c.Providers[0].Models = append(c.Providers[0].Models, c.Providers[0].Models[0])
		}, "duplicate model"},
		{"unknown provider in a tier", func(c *settings.Config) { c.Models["strong"] = []string{"nobody/qwen"} }, "not a configured provider/model"},
		{"unknown model in a tier", func(c *settings.Config) { c.Models["any"] = []string{"mini/nope"} }, "not a configured provider/model"},
		{"entry without a slash", func(c *settings.Config) { c.Models["any"] = []string{"mini"} }, "not a configured provider/model"},
		{"missing tier", func(c *settings.Config) { delete(c.Models, "standard") }, "tier standard has no entries"},
		{"unknown tier", func(c *settings.Config) { c.Models["huge"] = []string{"mini/qwen3.6:35b-a3b"} }, "unknown tier"},
		{"bad privacy mode", func(c *settings.Config) { c.Privacy.Mode = "open" }, "privacy.mode"},
		{"zero call timeout", func(c *settings.Config) { c.Defaults.CallTimeout = 0 }, "call_timeout"},
		{"negative coverage rounds", func(c *settings.Config) { c.Defaults.MaxCoverageRounds = -1 }, "max_coverage_rounds"},
		{"backoff max below initial", func(c *settings.Config) { c.RateLimits.BackoffMaxSeconds = 1 }, "backoff_max_seconds"},
		{"zero cooldown", func(c *settings.Config) { c.RateLimits.CooldownAfter429Seconds = 0 }, "cooldown_after_429_seconds"},
		{"bad human mode", func(c *settings.Config) { c.Human.Mode = "carrier pigeon" }, "human.mode"},
		{"bad reasoning effort", func(c *settings.Config) { c.Providers[0].ReasoningEffort = "max" }, "reasoning_effort"},
		{"no vault path", func(c *settings.Config) { c.Vault.Path = "" }, "vault.path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := settings.Default()
			c.mutate(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestLookups(t *testing.T) {
	c := settings.Default()
	if v, ok := c.Visibility("mini"); !ok || v != settings.Private {
		t.Errorf("mini = %v %v", v, ok)
	}
	if v, ok := c.Visibility("kilo"); !ok || v != settings.Public {
		t.Errorf("kilo = %v %v", v, ok)
	}
	if _, ok := c.Visibility("nobody"); ok {
		t.Error("unknown provider reported as known")
	}
	if mi, ok := c.ModelInfo("kilo/kilo-auto/free"); !ok || mi.ID != "kilo-auto/free" || mi.ContextTokens != 131072 {
		t.Errorf("ModelInfo = %+v %v", mi, ok)
	}
	if _, ok := c.ModelInfo("kilo/other"); ok {
		t.Error("unknown model reported as known")
	}
	for entry, want := range map[string][3]string{
		"kilo/kilo-auto/free":  {"kilo", "kilo-auto/free", "true"},
		"mini/qwen3.6:35b-a3b": {"mini", "qwen3.6:35b-a3b", "true"},
		"noslash":              {"", "", "false"},
		"/model":               {"", "", "false"},
		"prov/":                {"", "", "false"},
	} {
		p, m, ok := settings.SplitEntry(entry)
		if p != want[0] || m != want[1] || (want[2] == "true") != ok {
			t.Errorf("SplitEntry(%q) = %q %q %v", entry, p, m, ok)
		}
	}
}

func TestBuildProvidersResolvesKeysFromTheSecretSource(t *testing.T) {
	var auths = map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths[r.URL.Path] = r.Header.Get("Authorization")
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	cfg := settings.Default()
	cfg.Providers = []settings.ProviderConfig{
		{Name: "keyed", BaseURL: srv.URL + "/keyed", Visibility: settings.Public, MaxConcurrent: 1, APIKeySecret: "keyed-key",
			Models: []settings.ModelEntry{{ID: "m", ContextTokens: 1000}}},
		{Name: "open", BaseURL: srv.URL + "/open", Visibility: settings.Private, MaxConcurrent: 1,
			Models: []settings.ModelEntry{{ID: "m", ContextTokens: 1000}}},
	}
	cfg.Models = map[string][]string{"strong": {"open/m"}, "standard": {"open/m"}, "any": {"keyed/m"}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	var asked []string
	ps, err := cfg.BuildProviders(srv.Client(), func(name string) (string, error) {
		asked = append(asked, name)
		return "sk-VALUE", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps["keyed"].Name() != "keyed" || ps["open"].Models()[0].ID != "m" {
		t.Fatalf("providers = %v", ps)
	}
	if len(asked) != 1 || asked[0] != "keyed-key" {
		t.Errorf("secret source asked for %v, want only keyed-key", asked)
	}
	for _, name := range []string{"keyed", "open"} {
		if _, err := ps[name].Complete(context.Background(), provider.Request{Model: "m"}); err != nil {
			t.Fatal(err)
		}
	}
	if auths["/keyed/chat/completions"] != "Bearer sk-VALUE" || auths["/open/chat/completions"] != "" {
		t.Errorf("auth headers = %v", auths)
	}
}

func TestBuildProvidersSecretFailuresNameTheSecretNotItsValue(t *testing.T) {
	cfg := settings.Default()
	cfg.Providers[1].APIKeySecret = "kilo-key"
	if _, err := cfg.BuildProviders(nil, nil); err == nil {
		t.Error("a provider that needs a secret was built with no secret source")
	}
	_, err := cfg.BuildProviders(nil, func(string) (string, error) { return "sk-LEAK", errors.New("vault locked") })
	if err == nil || !strings.Contains(err.Error(), "kilo-key") || strings.Contains(err.Error(), "sk-LEAK") {
		t.Errorf("err = %v", err)
	}
}

func TestReasoningEffortDefaultsToNoneForMiniOnly(t *testing.T) {
	c := settings.Default()
	for _, p := range c.Providers {
		want := ""
		if p.Name == "mini" {
			want = "none"
		}
		if p.ReasoningEffort != want {
			t.Errorf("%s reasoning_effort = %q, want %q", p.Name, p.ReasoningEffort, want)
		}
	}
	path := filepath.Join(t.TempDir(), "gophermind.yaml")
	if _, err := settings.Load(path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "reasoning_effort: none") {
		t.Errorf("written defaults lack reasoning_effort: none")
	}
	if strings.Count(string(b), "reasoning_effort") != 1 {
		t.Errorf("reasoning_effort should appear once (mini only)")
	}
	for _, ok := range []string{"none", "low", "medium", "high"} {
		c := settings.Default()
		c.Providers[1].ReasoningEffort = ok
		if err := c.Validate(); err != nil {
			t.Errorf("%s rejected: %v", ok, err)
		}
	}
}
