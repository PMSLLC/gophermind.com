package projectrun

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gophermind/gophermind-lib/briefv2/envcheck"
	"gophermind/gophermind-lib/briefv2/sandbox"
	"gophermind/gophermind-lib/briefv2/settings"
)

// minFreeBytes is the free space the module cache's file system must have.
var minFreeBytes = envcheck.DefaultMinFreeBytes

// loadSettings never creates the file: settings.Load would write defaults.
func (p *pre) loadSettings() (*settings.Config, string, Check) {
	path, err := p.env.SettingsPath()
	if err != nil {
		return nil, "", fail("settings", "the settings path could not be resolved: "+err.Error(), "set GOPHERMIND_CONFIG_DIR or HOME")
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, path, fail("settings", "gophermind.yaml not found at "+path, "create it (see docs/briefv2/project-runbook.md)")
		}
		return nil, path, fail("settings", "gophermind.yaml cannot be read at "+path, "fix the permissions of "+path)
	}
	cfg, err := p.env.LoadSettings(path)
	if err != nil {
		return nil, path, fail("settings", err.Error(), "fix the line the error names in "+path+" (unknown keys are refused)")
	}
	return cfg, path, pass("settings", path)
}

const noSettings = "settings did not load"
const settingsFirst = "fix the settings check first"

// toolChecks are sandbox, go toolchain, git and disk space, in that order.
func (p *pre) toolChecks(cfg *settings.Config, settingsPath string) []Check {
	var out []Check

	switch {
	case cfg == nil:
		out = append(out, notChecked("sandbox", noSettings, settingsFirst))
	default:
		on, err := sandbox.Required(cfg.Executor.Sandbox, runtime.GOOS)
		switch {
		case err != nil:
			out = append(out, fail("sandbox", "a sandbox is required on this platform but the setting turns it off or is unsupported", "set executor.sandbox: off in "+settingsPath+" to run without one"))
		case !on:
			out = append(out, pass("sandbox", "setting off (recorded in the report)"))
		default:
			if err := p.env.SandboxPreflight(p.ctx); err != nil {
				out = append(out, fail("sandbox", "setting on but sandbox-exec is not available", "set executor.sandbox: off in "+settingsPath+" or make sandbox-exec runnable"))
			} else {
				out = append(out, pass("sandbox", "setting on, sandbox-exec available"))
			}
		}
	}

	if cfg == nil {
		out = append(out, notChecked("go toolchain", noSettings, settingsFirst))
	} else if _, ok := p.env.LookPath("go", cfg.Toolchain["PATH"]); ok {
		out = append(out, pass("go toolchain", ""))
	} else {
		dir := "<dir of go>"
		if g, ok := p.env.LookPath("go", p.env.Getenv("PATH")); ok {
			dir = filepath.Dir(g)
		}
		out = append(out, fail("go toolchain", "go was not found on toolchain.PATH", "add "+dir+" to toolchain.PATH in "+settingsPath))
	}

	if _, ok := p.env.LookPath("git", p.env.Getenv("PATH")); ok {
		out = append(out, pass("git", ""))
	} else {
		out = append(out, fail("git", "git was not found on PATH", "install git and put its directory on PATH"))
	}

	switch {
	case cfg == nil:
		out = append(out, notChecked("disk space", noSettings, settingsFirst))
	default:
		free, err := envcheck.ModuleCacheFree(cfg.Executor.GoModCache)
		switch {
		case err != nil:
			out = append(out, fail("disk space", "free space could not be read for the module cache", "check executor.go_mod_cache in "+settingsPath))
		case free < minFreeBytes:
			out = append(out, fail("disk space", fmt.Sprintf("less than %d MiB free for the module cache", minFreeBytes>>20), "free disk space on the volume that holds "+cfg.Executor.GoModCache))
		default:
			out = append(out, pass("disk space", ""))
		}
	}
	return out
}

// privacy: private_only, and every tier entry names a private provider.
func (p *pre) privacy(cfg *settings.Config, settingsPath string) Check {
	if cfg == nil {
		return notChecked("privacy", noSettings, settingsFirst)
	}
	fix := "set privacy.mode: private_only and name only private providers in models.strong, models.standard and models.any in " + settingsPath
	vis := map[string]settings.Visibility{}
	for _, pc := range cfg.Providers {
		vis[pc.Name] = pc.Visibility
	}
	var bad []string
	seen := map[string]bool{}
	for _, tier := range settings.Tiers {
		for _, entry := range cfg.Models[tier] {
			name, _, ok := settings.SplitEntry(entry)
			if !ok {
				name = entry
			}
			if v, known := vis[name]; (!known || v != settings.Private) && !seen[name] {
				seen[name] = true
				bad = append(bad, name)
			}
		}
	}
	switch {
	case cfg.Privacy.Mode != "private_only":
		d := "privacy.mode is " + cfg.Privacy.Mode + ", not private_only"
		if len(bad) > 0 {
			d += "; non-private providers in the tiers: " + strings.Join(bad, ", ")
		}
		return fail("privacy", d, fix)
	case len(bad) > 0:
		return fail("privacy", "non-private providers in the tiers: "+strings.Join(bad, ", "), fix)
	}
	return pass("privacy", "private_only, all tier providers private")
}

func (p *pre) providerChecks(probes []envcheck.ProbeResult, settingsPath string) []Check {
	var out []Check
	for _, r := range probes {
		name := "provider " + r.Provider
		switch {
		case !r.Answered:
			out = append(out, fail(name, "no base_url answered", "start the model server (the mini: ssh mini, then check ollama) or fix base_url and base_url_fallbacks for "+r.Provider+" in "+settingsPath))
		case r.Fallback:
			out = append(out, pass(name, "answered on fallback host "+r.Host))
		default:
			out = append(out, pass(name, "answered on "+r.Host))
		}
	}
	return out
}

func (p *pre) binary() Check {
	v := p.env.Version()
	detail := fmt.Sprintf("version %s commit %s", v.Version, v.Commit)
	if v.Commit == "" || v.Commit == "none" || !strings.HasPrefix(v.Commit, p.o.ExpectBinaryCommit) {
		return fail("binary", detail+", expected commit "+p.o.ExpectBinaryCommit, "rebuild with scripts/build-dev-binary.sh")
	}
	return pass("binary", detail)
}
