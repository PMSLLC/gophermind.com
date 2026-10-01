package executor

import (
	"sort"
	"strings"

	"gophermind/gophermind-lib/briefv2/acceptcheck"
)

// vacuousError is the tripwire for acceptance root tests that cannot prove the
// server: it names bullet ids only, never a command.
type vacuousError struct{ IDs []string }

func (e *vacuousError) Error() string {
	return "executor: the root tests of " + strings.Join(e.IDs, ", ") + " do not exercise the built server (acceptance_vacuous)"
}

// vacuousCommand is acceptcheck.Vacuous: the shell-aware check lives in the
// shared package the planner also uses.
func vacuousCommand(cmd string, bins []string) bool { return acceptcheck.Vacuous(cmd, bins) }

// vacuousBullets is the ids of the acceptance bullets that have a vacuous root test.
func vacuousBullets(p acceptPlan, bins []string) []string {
	var ids []string
	for _, pb := range p.acceptance {
		for _, t := range pb.tests {
			if vacuousCommand(t.Command, bins) {
				ids = append(ids, pb.req.ID)
				break
			}
		}
	}
	return ids
}

// unsetNames are the declared secret and env names a bullet's text says it runs
// without, by the exact phrase "with NAME unset" (case sensitive).
func (rc *runCtx) unsetNames(text string) []string {
	declared := map[string]bool{}
	for _, s := range rc.plan.Brief.Front.Secrets {
		declared[s.Name] = true
	}
	for _, e := range rc.plan.Brief.Front.Env {
		declared[e.Name] = true
	}
	out := acceptcheck.UnsetNames(text, declared)
	sort.Strings(out)
	return out
}
