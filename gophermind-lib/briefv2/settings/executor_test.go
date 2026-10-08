package settings_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"gophermind/gophermind-lib/briefv2/settings"
)

func specDefaults() settings.ExecutorConfig {
	return settings.ExecutorConfig{
		Workers: 1, FixAttempts: 2, RepairRounds: 2, AcceptanceRepairRounds: 2,
		TestTimeoutSeconds: 120, AcceptanceTimeoutSeconds: 300, StaleClaimSeconds: 120,
		OutputCapBytes: 65536, HeartbeatSeconds: 30, GoModCache: "~/.gophermind/gomodcache",
		MaxRunMinutes: 720, Sandbox: "on", Artifacts: "on", ArtifactMaxBytes: 65536,
		Proxy: settings.ExecutorProxy{Listen: "127.0.0.1:0", Log: "proxy.log"},
	}
}

func TestExecutorSettingsDefaults(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	if got := settings.DefaultExecutor(); !reflect.DeepEqual(got, specDefaults()) {
		t.Fatalf("DefaultExecutor = %+v", got)
	}
	d := settings.Default()
	if !reflect.DeepEqual(d.Executor, specDefaults()) {
		t.Errorf("Default().Executor = %+v", d.Executor)
	}
	if !reflect.DeepEqual(d.Toolchain, map[string]string{"PATH": "/usr/local/go/bin:/usr/bin:/bin"}) {
		t.Errorf("Default().Toolchain = %v", d.Toolchain)
	}

	// An old-format file (no executor, no toolchain, no reasoning_effort) loads,
	// validates, equals the defaults plus what the file says, and is not rewritten.
	old := filepath.Join(t.TempDir(), "gophermind.yaml")
	fresh := filepath.Join(t.TempDir(), "fresh.yaml")
	if _, err := settings.Load(fresh); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(fresh)
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "executor")
	delete(m, "toolchain")
	for _, p := range m["providers"].([]any) {
		delete(p.(map[string]any), "reasoning_effort")
	}
	oldBody, _ := yaml.Marshal(m)
	if err := os.WriteFile(old, oldBody, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := settings.Load(old)
	if err != nil {
		t.Fatalf("old-format file: %v", err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Executor, specDefaults()) || c.Toolchain["PATH"] == "" {
		t.Errorf("old file did not get executor defaults: %+v %v", c.Executor, c.Toolchain)
	}
	if c.Providers[0].ReasoningEffort != "" {
		t.Errorf("old file gained reasoning_effort %q", c.Providers[0].ReasoningEffort)
	}
	after, _ := os.ReadFile(old)
	if string(after) != string(oldBody) {
		t.Error("Load rewrote an old-format file")
	}

	// A fresh file carries both sections and still the mini reasoning_effort.
	if !strings.Contains(string(b), "executor:") || !strings.Contains(string(b), "toolchain:") {
		t.Errorf("written defaults lack executor: or toolchain:\n%s", b)
	}
	if !strings.Contains(string(b), "reasoning_effort: none") {
		t.Error("written defaults lack reasoning_effort: none")
	}
	again, err := settings.Load(fresh)
	if err != nil || !reflect.DeepEqual(again, settings.Default()) {
		t.Errorf("round trip differs: %v", err)
	}

	// A partial section keeps its values and fills the rest.
	part := filepath.Join(t.TempDir(), "p.yaml")
	pm := map[string]any{}
	_ = yaml.Unmarshal(oldBody, &pm)
	pm["executor"] = map[string]any{"fix_attempts": 5}
	pb, _ := yaml.Marshal(pm)
	if err := os.WriteFile(part, pb, 0o600); err != nil {
		t.Fatal(err)
	}
	pc, err := settings.Load(part)
	if err != nil || pc.Executor.FixAttempts != 5 || pc.Executor.Workers != 1 || pc.Executor.Sandbox != "on" {
		t.Errorf("partial section: %+v %v", pc, err)
	}
}

func TestExecutorSandboxOnDecodesToString(t *testing.T) {
	var e settings.ExecutorConfig
	if err := yaml.Unmarshal([]byte("sandbox: on\n"), &e); err != nil {
		t.Fatal(err)
	}
	if e.Sandbox != "on" {
		t.Errorf("sandbox: on decoded to %q", e.Sandbox)
	}
	if err := yaml.Unmarshal([]byte("sandbox: off\n"), &e); err != nil || e.Sandbox != "off" {
		t.Errorf("sandbox: off decoded to %q, %v", e.Sandbox, err)
	}
}

func TestExecutorSettingsValidate(t *testing.T) {
	ints := map[string]func(*settings.ExecutorConfig, int){
		"workers":                    func(e *settings.ExecutorConfig, v int) { e.Workers = v },
		"fix_attempts":               func(e *settings.ExecutorConfig, v int) { e.FixAttempts = v },
		"repair_rounds":              func(e *settings.ExecutorConfig, v int) { e.RepairRounds = v },
		"acceptance_repair_rounds":   func(e *settings.ExecutorConfig, v int) { e.AcceptanceRepairRounds = v },
		"test_timeout_seconds":       func(e *settings.ExecutorConfig, v int) { e.TestTimeoutSeconds = v },
		"acceptance_timeout_seconds": func(e *settings.ExecutorConfig, v int) { e.AcceptanceTimeoutSeconds = v },
		"stale_claim_seconds":        func(e *settings.ExecutorConfig, v int) { e.StaleClaimSeconds = v },
		"output_cap_bytes":           func(e *settings.ExecutorConfig, v int) { e.OutputCapBytes = v },
		"heartbeat_seconds":          func(e *settings.ExecutorConfig, v int) { e.HeartbeatSeconds = v },
		"max_run_minutes":            func(e *settings.ExecutorConfig, v int) { e.MaxRunMinutes = v },
		"artifact_max_bytes":         func(e *settings.ExecutorConfig, v int) { e.ArtifactMaxBytes = v },
	}
	type tc struct {
		name   string
		mutate func(*settings.Config)
		errHas string // empty means valid
	}
	var cases []tc
	for key, set := range ints {
		key, set := key, set
		for _, v := range []int{0, -1} {
			v := v
			cases = append(cases, tc{key + " invalid", func(c *settings.Config) { set(&c.Executor, v) }, "executor." + key})
		}
	}
	cases = append(cases,
		tc{"sandbox maybe", func(c *settings.Config) { c.Executor.Sandbox = "maybe" }, "executor.sandbox"},
		tc{"sandbox off ok", func(c *settings.Config) { c.Executor.Sandbox = "off" }, ""},
		tc{"listen 0.0.0.0", func(c *settings.Config) { c.Executor.Proxy.Listen = "0.0.0.0:0" }, "executor.proxy.listen"},
		tc{"listen ipv4 loopback", func(c *settings.Config) { c.Executor.Proxy.Listen = "127.0.0.1:0" }, ""},
		tc{"listen ipv6 loopback", func(c *settings.Config) { c.Executor.Proxy.Listen = "[::1]:0" }, ""},
		tc{"listen localhost", func(c *settings.Config) { c.Executor.Proxy.Listen = "localhost:0" }, ""},
		tc{"listen no port", func(c *settings.Config) { c.Executor.Proxy.Listen = "127.0.0.1" }, "executor.proxy.listen"},
		tc{"listen empty", func(c *settings.Config) { c.Executor.Proxy.Listen = "" }, "executor.proxy.listen"},
		tc{"listen public name", func(c *settings.Config) { c.Executor.Proxy.Listen = "example.com:0" }, "executor.proxy.listen"},
		tc{"proxy log empty", func(c *settings.Config) { c.Executor.Proxy.Log = "" }, "executor.proxy.log"},
		tc{"proxy log path", func(c *settings.Config) { c.Executor.Proxy.Log = "../x.log" }, "executor.proxy.log"},
		tc{"go_mod_cache empty", func(c *settings.Config) { c.Executor.GoModCache = "" }, "executor.go_mod_cache"},
		tc{"absurd workers", func(c *settings.Config) { c.Executor.Workers = 1000 }, "executor.workers"},
		tc{"absurd fix_attempts", func(c *settings.Config) { c.Executor.FixAttempts = 1000 }, "executor.fix_attempts"},
		tc{"absurd max_run_minutes", func(c *settings.Config) { c.Executor.MaxRunMinutes = 10_000_000 }, "executor.max_run_minutes"},
		tc{"absurd output cap", func(c *settings.Config) { c.Executor.OutputCapBytes = 1 << 40 }, "executor.output_cap_bytes"},
		tc{"toolchain empty PATH", func(c *settings.Config) { c.Toolchain = map[string]string{"PATH": ""} }, "toolchain.PATH"},
		tc{"toolchain no PATH", func(c *settings.Config) { c.Toolchain = map[string]string{"X": "y"} }, "toolchain.PATH"},
		tc{"toolchain extra key ok", func(c *settings.Config) { c.Toolchain = map[string]string{"PATH": "/bin", "LANG": "C"} }, ""},
	)
	for _, c := range cases {
		cfg := settings.Default()
		c.mutate(cfg)
		err := cfg.Validate()
		switch {
		case c.errHas == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.errHas != "" && (err == nil || !strings.Contains(err.Error(), c.errHas)):
			t.Errorf("%s: error %v, want it to name %s", c.name, err, c.errHas)
		}
	}
}

func TestExecutorErrorsNeverEchoValues(t *testing.T) {
	cfg := settings.Default()
	cfg.Executor.Sandbox = "CANARY-SANDBOX"
	cfg.Executor.Proxy.Listen = "CANARY-LISTEN"
	cfg.Toolchain = map[string]string{"PATH": "", "SECRET": "CANARY-TOOL"}
	if err := cfg.Validate(); err == nil || strings.Contains(err.Error(), "CANARY") {
		t.Errorf("error %v", err)
	}
	cfg.Toolchain = map[string]string{"PATH": "/bin"}
	if err := cfg.Validate(); err == nil || strings.Contains(err.Error(), "CANARY") {
		t.Errorf("error %v", err)
	}
	cfg.Executor.Sandbox = "on"
	if err := cfg.Validate(); err == nil || strings.Contains(err.Error(), "CANARY") {
		t.Errorf("error %v", err)
	}
}

func TestExecutorLoadRejectsUnknownKeyAndNegative(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, "fresh.yaml")
	if _, err := settings.Load(fresh); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(fresh)
	for name, body := range map[string]string{
		"unknown executor key": strings.Replace(string(b), "executor:\n", "executor:\n    wrokers: 2\n", 1),
		"negative":             strings.Replace(string(b), "workers: 1", "workers: -1", 1),
		"bad sandbox":          strings.Replace(string(b), "sandbox: \"on\"", "sandbox: maybe", 1),
	} {
		if body == string(b) {
			t.Fatalf("%s: edit did not apply:\n%s", name, b)
		}
		p := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".yaml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := settings.Load(p); err == nil {
			t.Errorf("%s: Load accepted it", name)
		}
	}
}

func TestExecutorExplicitZeroCountsRejected(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, "fresh.yaml")
	if _, err := settings.Load(fresh); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(fresh)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"fix_attempts", "repair_rounds", "acceptance_repair_rounds"} {
		var m map[string]any
		if err := yaml.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		m["executor"].(map[string]any)[key] = 0
		body, _ := yaml.Marshal(m)
		p := filepath.Join(dir, key+"-zero.yaml")
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := settings.Load(p)
		if err == nil || !strings.Contains(err.Error(), "executor."+key+" must be at least 1") {
			t.Errorf("%s: explicit 0 gave %v", key, err)
		}

		// Omitted key takes the default, and the file is not rewritten.
		delete(m["executor"].(map[string]any), key)
		body, _ = yaml.Marshal(m)
		q := filepath.Join(dir, key+"-omitted.yaml")
		if err := os.WriteFile(q, body, 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := settings.Load(q)
		if err != nil || c.Executor.FixAttempts != 2 || c.Executor.RepairRounds != 2 || c.Executor.AcceptanceRepairRounds != 2 {
			t.Errorf("%s omitted: %+v %v", key, c, err)
		}
		if after, _ := os.ReadFile(q); string(after) != string(body) {
			t.Errorf("%s omitted: file rewritten", key)
		}
	}
}

func TestExecutorProxyLogSeparators(t *testing.T) {
	for _, bad := range []string{"a/b.log", `a\b.log`, "..", ".", `..\x`, "/abs.log"} {
		c := settings.Default()
		c.Executor.Proxy.Log = bad
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "executor.proxy.log") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// Leaf model loops are serialised (ruling): any worker count but 1 is refused
// with one fixed message naming the key.
func TestExecutorWorkersMustBeOne(t *testing.T) {
	const want = "executor.workers must be 1 until leaf-isolated trees exist"
	for _, n := range []int{2, 3, 64} {
		c := settings.Default()
		c.Executor.Workers = n
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("workers %d: %v, want %q", n, err, want)
		}
	}
	c := settings.Default()
	if c.Executor.Workers != 1 || c.Validate() != nil {
		t.Errorf("the default must be 1 and valid, got %d", c.Executor.Workers)
	}
}

func TestExecutorArtifactsSetting(t *testing.T) {
	d := settings.DefaultExecutor()
	if d.Artifacts != "on" || d.ArtifactMaxBytes != 65536 {
		t.Fatalf("defaults = %q, %d; want on, 65536", d.Artifacts, d.ArtifactMaxBytes)
	}
	var e settings.ExecutorConfig
	if err := yaml.Unmarshal([]byte("artifacts: off\nartifact_max_bytes: 1000\n"), &e); err != nil || e.Artifacts != "off" || e.ArtifactMaxBytes != 1000 {
		t.Errorf("decoded %q %d, %v", e.Artifacts, e.ArtifactMaxBytes, err)
	}
	c := settings.Default()
	c.Executor.Artifacts = "maybe"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "executor.artifacts") {
		t.Errorf("artifacts: maybe must be refused, got %v", err)
	}
}
