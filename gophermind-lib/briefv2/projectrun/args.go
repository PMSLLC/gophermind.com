package projectrun

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var secretNameRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

const usage = "usage: project <brief-path> [--repo <path>] [--generate NAME=KIND] [--require-private] [--expect-head <rev>] [--graded] [--expect-binary-commit <sha>] [--resume] [--attended] [--preflight-only] [--print-state-paths]"

var (
	boolFlags  = map[string]bool{"attended": true, "resume": true, "graded": true, "require-private": true, "preflight-only": true, "print-state-paths": true}
	valueFlags = map[string]bool{"repo": true, "generate": true, "expect-head": true, "expect-binary-commit": true}
)

// ParseArgs reads the arguments after "project". Flags may come before or
// after the one positional, the brief path. An error never repeats a flag
// value, since a value may be a secret.
func ParseArgs(args []string, allowAttended bool) (Options, error) {
	var o Options
	var pos []string
	genN := 0
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			pos = append(pos, a)
			continue
		}
		if !strings.HasPrefix(a, "--") {
			return o, errors.New("unknown flag (flags are spelled with two dashes)")
		}
		name := a[2:]
		val, hasVal := "", false
		if k := strings.IndexByte(name, '='); k >= 0 {
			name, val, hasVal = name[:k], name[k+1:], true
		}
		if (boolFlags[name] || valueFlags[name]) && seen[name] {
			return o, fmt.Errorf("flag --%s given twice", name)
		}
		seen[name] = true
		switch {
		case boolFlags[name]:
			if hasVal {
				return o, fmt.Errorf("flag --%s takes no value", name)
			}
			switch name {
			case "attended":
				o.Attended = true
			case "resume":
				o.Resume = true
			case "graded":
				o.Graded = true
			case "require-private":
				o.RequirePrivate = true
			case "preflight-only":
				o.PreflightOnly = true
			case "print-state-paths":
				o.PrintStatePaths = true
			}
		case valueFlags[name]:
			if !hasVal {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
					return o, fmt.Errorf("flag --%s needs a value", name)
				}
				i++
				val = args[i]
			}
			if val == "" {
				return o, fmt.Errorf("flag --%s needs a value", name)
			}
			switch name {
			case "repo":
				o.Repo = val
			case "expect-head":
				o.ExpectHead = val
				o.Graded = true
			case "expect-binary-commit":
				o.ExpectBinaryCommit = val
			case "generate":
				genN++
				k := strings.IndexByte(val, '=')
				if k < 0 || !secretNameRE.MatchString(val[:k]) || (val[k+1:] != "hex32" && val[k+1:] != "placeholder") {
					return o, fmt.Errorf("--generate #%d: want NAME=KIND with NAME like [A-Z][A-Z0-9_]* and KIND hex32 or placeholder", genN)
				}
				if o.Generate == nil {
					o.Generate = map[string]string{}
				}
				if _, dup := o.Generate[val[:k]]; dup {
					return o, fmt.Errorf("--generate #%d: a name is given twice", genN)
				}
				o.Generate[val[:k]] = val[k+1:]
			}
		default:
			return o, fmt.Errorf("unknown flag --%s", name)
		}
	}
	switch len(pos) {
	case 0:
		return o, errors.New(usage)
	case 1:
		o.BriefPath = pos[0]
	default:
		return o, errors.New(`the v1 form "/project <name> <brief>" is gone: give the brief path only (the v1 planner is /plan-v1)`)
	}
	if o.Attended && !allowAttended {
		return o, errors.New("attended runs need a terminal and are only available from the gophermind project command")
	}
	if o.PreflightOnly && o.PrintStatePaths {
		return o, errors.New("--preflight-only and --print-state-paths cannot be combined")
	}
	if o.Graded {
		switch {
		case o.ExpectHead == "":
			return o, errors.New("--graded needs --expect-head <rev>")
		case o.Resume:
			return o, errors.New("a graded attempt never resumes")
		case o.Attended:
			return o, errors.New("a graded attempt cannot be attended")
		}
	}
	return o, nil
}
