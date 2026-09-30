package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/blackboard"
	"gophermind/gophermind-lib/briefv2/db"
	"gophermind/gophermind-lib/briefv2/events"
	"gophermind/gophermind-lib/briefv2/ledger"
	"gophermind/gophermind-lib/briefv2/provider"
	"gophermind/gophermind-lib/briefv2/router"
	"gophermind/gophermind-lib/briefv2/settings"
)

type stubCaller struct{}

func (stubCaller) CallParsed(context.Context, router.CallInfo, provider.Request, func(string) error) (router.Result, error) {
	return router.Result{}, errors.New("stub")
}

func validOptions(t *testing.T) Options {
	t.Helper()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return Options{
		RunDir: filepath.Join(repo, ".gophermind", "gm-2026-09-30-901"), Repo: repo, Caller: stubCaller{},
		Board: blackboard.NewSQLite(d), Ledger: ledger.NewSQLite(d), Settings: settings.Default(),
	}
}

func TestOptionsValidate(t *testing.T) {
	if o := validOptions(t); o.validate() != nil {
		t.Fatalf("valid options refused: %v", o.validate())
	}

	required := []struct {
		field string
		clear func(*Options)
	}{
		{"RunDir", func(o *Options) { o.RunDir = "" }},
		{"Repo", func(o *Options) { o.Repo = "" }},
		{"Caller", func(o *Options) { o.Caller = nil }},
		{"Board", func(o *Options) { o.Board = nil }},
		{"Ledger", func(o *Options) { o.Ledger = nil }},
		{"Settings", func(o *Options) { o.Settings = nil }},
	}
	for _, c := range required {
		t.Run("missing "+c.field, func(t *testing.T) {
			o := validOptions(t)
			c.clear(&o)
			err := o.validate()
			want := "executor: Options." + c.field + " is required"
			if err == nil || err.Error() != want {
				t.Fatalf("validate() = %v, want %q", err, want)
			}
		})
	}

	t.Run("relative paths", func(t *testing.T) {
		o := validOptions(t)
		o.Repo = "relative/repo"
		if err := o.validate(); err == nil || !strings.Contains(err.Error(), "Options.Repo must be an absolute path") {
			t.Errorf("relative Repo: %v", err)
		}
		o = validOptions(t)
		o.RunDir = "relative/run"
		if err := o.validate(); err == nil || !strings.Contains(err.Error(), "Options.RunDir must be an absolute path") {
			t.Errorf("relative RunDir: %v", err)
		}
	})

	t.Run("repo must exist and be a repository root", func(t *testing.T) {
		o := validOptions(t)
		o.Repo = filepath.Join(o.Repo, "missing")
		if err := o.validate(); err == nil || !strings.Contains(err.Error(), "Options.Repo is not a directory") {
			t.Errorf("missing Repo: %v", err)
		}
		o = validOptions(t)
		if err := os.Remove(filepath.Join(o.Repo, ".git")); err != nil {
			t.Fatal(err)
		}
		if err := o.validate(); err == nil || !strings.Contains(err.Error(), "Options.Repo is not the root of a git repository") {
			t.Errorf("Repo without .git: %v", err)
		}
	})

	t.Run("settings are validated", func(t *testing.T) {
		o := validOptions(t)
		o.Settings.Executor.Workers = -1
		if err := o.validate(); err == nil || !strings.Contains(err.Error(), "executor.workers") {
			t.Errorf("bad settings: %v", err)
		}
		o = validOptions(t)
		o.Settings.Executor.Sandbox = "maybe"
		if err := o.validate(); err == nil || !strings.Contains(err.Error(), "executor.sandbox") {
			t.Errorf("sandbox not on or off: %v", err)
		}
	})

	t.Run("defaults are filled and Git is left alone", func(t *testing.T) {
		o := validOptions(t)
		if o.Sink != nil || o.Now != nil || o.Git != nil {
			t.Fatal("test setup: Sink, Now and Git must start nil")
		}
		if err := o.validate(); err != nil {
			t.Fatal(err)
		}
		if o.Sink != events.Nop {
			t.Error("a nil Sink must become events.Nop")
		}
		if o.Now == nil || o.Now().IsZero() {
			t.Error("a nil Now must become time.Now")
		}
		if o.Git != nil {
			t.Error("validate must leave Git nil: startRun builds it once it knows the run id")
		}
		col := events.NewCollector()
		o = validOptions(t)
		o.Sink = col
		if err := o.validate(); err != nil {
			t.Fatal(err)
		}
		if o.Sink != events.Sink(col) {
			t.Error("a given Sink must be kept")
		}
	})
}

// TestOptionsNeverPrintSecrets: the values a run holds (the vault, the
// caller, the settings) never reach a log line through %v, %+v, %#v or %s.
func TestOptionsNeverPrintSecrets(t *testing.T) {
	o := validOptions(t)
	mem := newMemSecrets()
	_ = mem.Set("run/x", "GREETER_TOKEN", canarySecret)
	o.Secrets = mem
	o.Settings.Providers = []settings.ProviderConfig{{Name: "p", APIKeySecret: canarySecret}}
	for _, v := range []any{o, &o} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
			got := fmt.Sprintf(verb, v)
			if strings.Contains(got, canarySecret) {
				t.Errorf("%s of %T leaks a secret: %s", verb, v, got)
			}
			if !strings.Contains(got, o.RunDir) {
				t.Errorf("%s of %T does not name the run folder: %s", verb, v, got)
			}
		}
	}
}
