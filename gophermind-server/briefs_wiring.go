package main

import (
	"fmt"
	"os/exec"
	"strings"

	"gophermind/gophermind-lib/serve"
)

// briefOptions adds the browser brief settings of cfg to opt. With no
// BriefsDir the routes stay off. The launcher and signal prefixes are plain
// words, never run through a shell.
func briefOptions(cfg serverConfig, opt serve.Options) serve.Options {
	if cfg.BriefsDir == "" {
		return opt
	}
	opt.BriefsDir = cfg.BriefsDir
	if prefix := strings.Fields(cfg.BriefCmd); len(prefix) > 0 {
		opt.BriefCommand = func(args []string) *exec.Cmd {
			return exec.Command(prefix[0], append(append([]string(nil), prefix[1:]...), args...)...)
		}
	}
	if kill := strings.Fields(cfg.BriefKillCmd); len(kill) > 0 {
		opt.BriefSignal = func(pgid int, force bool) error {
			sig := "-TERM"
			if force {
				sig = "-KILL"
			}
			args := append(append([]string(nil), kill[1:]...), sig, "--", fmt.Sprintf("-%d", pgid))
			return exec.Command(kill[0], args...).Run()
		}
	}
	return opt
}
