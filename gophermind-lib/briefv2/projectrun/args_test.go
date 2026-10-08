package projectrun

import (
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_") {
			os.Unsetenv(strings.SplitN(kv, "=", 2)[0])
		}
	}
	os.Exit(m.Run())
}

func TestParseArgsTable(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		attended bool
		wantErr  string
		check    func(t *testing.T, o Options)
	}{
		{name: "flags before", args: []string{"--repo", "/r", "--resume", "b.md"}, check: func(t *testing.T, o Options) {
			if o.BriefPath != "b.md" || o.Repo != "/r" || !o.Resume {
				t.Fatalf("%+v", o)
			}
		}},
		{name: "flags after", args: []string{"b.md", "--repo=/r", "--require-private"}, check: func(t *testing.T, o Options) {
			if o.BriefPath != "b.md" || o.Repo != "/r" || !o.RequirePrivate {
				t.Fatalf("%+v", o)
			}
		}},
		{name: "generate repeated", args: []string{"b.md", "--generate", "A_B=hex32", "--generate", "C1=placeholder"}, check: func(t *testing.T, o Options) {
			if len(o.Generate) != 2 || o.Generate["A_B"] != "hex32" || o.Generate["C1"] != "placeholder" {
				t.Fatalf("%+v", o.Generate)
			}
		}},
		{name: "bad kind", args: []string{"b.md", "--generate", "A=zzz-secret"}, wantErr: "--generate"},
		{name: "bad name", args: []string{"b.md", "--generate", "a=hex32"}, wantErr: "--generate"},
		{name: "no equals", args: []string{"b.md", "--generate", "supersecretvalue"}, wantErr: "--generate"},
		{name: "duplicate name", args: []string{"b.md", "--generate", "A=hex32", "--generate", "A=placeholder"}, wantErr: "--generate"},
		{name: "attended refused", args: []string{"b.md", "--attended"}, wantErr: "attended runs need a terminal and are only available from the gophermind project command"},
		{name: "attended allowed", args: []string{"b.md", "--attended"}, attended: true, check: func(t *testing.T, o Options) {
			if !o.Attended {
				t.Fatal("not attended")
			}
		}},
		{name: "both query flags", args: []string{"b.md", "--preflight-only", "--print-state-paths"}, wantErr: "--preflight-only"},
		{name: "preflight only", args: []string{"b.md", "--preflight-only"}, check: func(t *testing.T, o Options) {
			if !o.PreflightOnly {
				t.Fatal("not set")
			}
		}},
		{name: "two positionals", args: []string{"name", "b.md"}, wantErr: `the v1 form "/project <name> <brief>" is gone: give the brief path only (the v1 planner is /plan-v1)`},
		{name: "none", args: nil, wantErr: "usage"},
		{name: "unknown flag", args: []string{"b.md", "--bogus=hunter2"}, wantErr: "--bogus"},
		{name: "missing value", args: []string{"b.md", "--repo"}, wantErr: "--repo"},
		{name: "binary commit", args: []string{"b.md", "--expect-binary-commit", "abc"}, check: func(t *testing.T, o Options) {
			if o.ExpectBinaryCommit != "abc" {
				t.Fatal(o.ExpectBinaryCommit)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o, err := ParseArgs(c.args, c.attended)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}
				for _, secret := range []string{"zzz-secret", "supersecretvalue", "hunter2"} {
					if strings.Contains(err.Error(), secret) {
						t.Fatalf("error echoes a flag value: %v", err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			c.check(t, o)
		})
	}
}

func TestParseArgsGraded(t *testing.T) {
	o, err := ParseArgs([]string{"b.md", "--expect-head", "abc"}, false)
	if err != nil || !o.Graded || o.ExpectHead != "abc" {
		t.Fatalf("%+v %v", o, err)
	}
	for name, args := range map[string][]string{
		"graded alone":     {"b.md", "--graded"},
		"graded resume":    {"b.md", "--graded", "--expect-head", "x", "--resume"},
		"graded attended":  {"b.md", "--graded", "--expect-head", "x", "--attended"},
		"implied attended": {"b.md", "--expect-head", "x", "--attended"},
	} {
		if _, err := ParseArgs(args, true); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	_, err = ParseArgs([]string{"b.md", "--graded"}, false)
	if err == nil || !strings.Contains(err.Error(), "--graded needs --expect-head <rev>") {
		t.Fatalf("err = %v", err)
	}
	_, err = ParseArgs([]string{"b.md", "--graded", "--expect-head", "x", "--resume"}, false)
	if err == nil || !strings.Contains(err.Error(), "a graded attempt never resumes") {
		t.Fatalf("err = %v", err)
	}
}

func TestExitCodes(t *testing.T) {
	got := []int{ExitVerified, ExitFailed, ExitInvalid, ExitWaiting, ExitEscalated, ExitInterrupted, ExitPreflight, ExitFault}
	for i, v := range got {
		if v != i {
			t.Fatalf("constant %d = %d", i, v)
		}
	}
	for _, c := range []struct {
		status, reason string
		want           int
	}{
		{"verified", "", 0}, {"failed", "", 1}, {"escalated", "", 4},
		{"escalated", "waiting_on_human", 3}, {"interrupted", "", 5},
		{"preflight_failed", "", 6}, {"harness_fault", "", 7}, {"invalid_brief", "", 2},
	} {
		if g := ExitCode(c.status, c.reason); g != c.want {
			t.Errorf("ExitCode(%q,%q) = %d, want %d", c.status, c.reason, g, c.want)
		}
	}
}
