package projectrun

import (
	"context"
	"fmt"
	"io"
	"strings"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/envcheck"
	"gophermind/gophermind-lib/briefv2/settings"
)

// Check is one line of the pre-plan preflight.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"` // ids, paths, counts; never a secret, never command output
	Fix    string `json:"fix"`    // the exact command or setting line to fix it; empty when OK
}

// PreflightResult is what Preflight found and what later stages reuse.
type PreflightResult struct {
	Checks []Check
	Config *settings.Config       // resolved: base_url replaced by the host that answered; nil when settings failed
	Probes []envcheck.ProbeResult // which host answered, per provider
	Store  SecretStore            // the opened vault, nil when none was needed or it did not open; never put in a Check
}

// Failed returns the checks that did not pass, in order.
func Failed(cs []Check) []Check {
	var out []Check
	for _, c := range cs {
		if !c.OK {
			out = append(out, c)
		}
	}
	return out
}

// oneLine keeps the first line of s, printable ASCII only. Unlike
// envcheck.SafeLine it does not cut at 160 bytes, so a path survives whole.
func oneLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	b := []byte(s)
	for i, c := range b {
		if c < 0x20 || c > 0x7e {
			b[i] = '?'
		}
	}
	if len(b) > 600 {
		b = b[:600]
	}
	return string(b)
}

func pass(name, detail string) Check {
	return Check{Name: name, OK: true, Detail: oneLine(detail)}
}

func fail(name, detail, fix string) Check {
	return Check{Name: name, Detail: oneLine(detail), Fix: fix}
}

// notChecked is the failure of a check whose input did not load.
func notChecked(name, why, fix string) Check { return fail(name, "not checked: "+why, fix) }

// PrintPreflight writes the result: one line when everything passed, else the
// numbered list of what a human must provide. Warnings print as "warning:" lines.
func PrintPreflight(w io.Writer, cs []Check) {
	failed := Failed(cs)
	checks := 0
	for _, c := range cs {
		if c.Name != "warning" {
			checks++
		}
	}
	if len(failed) == 0 {
		fmt.Fprintf(w, "preflight: ok (%d checks)\n", checks)
	} else {
		fmt.Fprintf(w, "preflight: FAILED (%d missing)\n", len(failed))
		fmt.Fprintln(w, "What a human must provide, in this order:")
		for i, c := range failed {
			fmt.Fprintf(w, "  %d. %s: %s\n", i+1, c.Name, c.Detail)
			fmt.Fprintf(w, "     Fix: %s\n", c.Fix)
		}
	}
	for _, c := range cs {
		if c.Name == "warning" {
			fmt.Fprintf(w, "warning: %s\n", c.Detail)
		}
	}
	if len(failed) > 0 {
		fmt.Fprintln(w, "This is not a run failure: nothing was planned or built, no run folder was created, and the attempt count is unchanged. Provide the items above and run /project again.")
	}
}

// Preflight runs every pre-plan check, in a fixed order and with no early
// exit, so one call lists everything a human must provide. It makes no model
// call, builds no run-state backend and writes no file.
func Preflight(ctx context.Context, o Options, env Env, b *brief.Brief) PreflightResult {
	p := &pre{ctx: ctx, o: o, env: env, b: b}
	var res PreflightResult
	add := func(cs ...Check) { res.Checks = append(res.Checks, cs...) }

	repoChecks := p.repoChecks()
	stale, leftovers := p.staleState()
	cfg, settingsPath, settingsCheck := p.loadSettings()

	add(repoChecks...)
	add(stale)
	add(p.landing())
	if o.Graded {
		add(p.graded(leftovers))
	}
	add(p.toolChecks(cfg, settingsPath)...)
	add(settingsCheck)
	if o.RequirePrivate {
		add(p.privacy(cfg, settingsPath))
	}

	if cfg != nil {
		res.Config, res.Probes = env.Probe(ctx, cfg)
		add(p.providerChecks(res.Probes, settingsPath)...)
	}
	vs := p.vault(res.Config)
	res.Store = vs.store
	add(vs.keyChecks...)
	if o.ExpectBinaryCommit != "" {
		add(p.binary())
	}
	add(vs.vaultChecks...)
	add(vs.secretChecks...)
	add(p.databaseChecks(vs.store)...)
	for _, w := range b.UndeclaredSecrets() {
		add(pass("warning", fmt.Sprintf("line %d: %s looks like a secret the brief does not declare", w.Line, w.Token)))
	}
	return res
}

type pre struct {
	ctx context.Context
	o   Options
	env Env
	b   *brief.Brief
}
