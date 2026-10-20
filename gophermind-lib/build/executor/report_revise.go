package executor

import (
	"os"

	"gophermind/gophermind-lib/build/report"
	"gophermind/gophermind-lib/build/revlog"
)

// revisedLeaves reads the revision log of every leaf and summarizes it for the
// report. A log that cannot be read leaves that leaf out and tells the sink; a
// report must never fail because of it.
func (rc *runCtx) revisedLeaves() []report.RevisedLeaf {
	var out []report.RevisedLeaf
	for _, l := range rc.plan.Leaves {
		dir, err := revlog.NodeDir(rc.o.RunDir, l.ID)
		if err != nil {
			rc.emit("warning", l.ID, "the revision log is not readable")
			continue
		}
		if _, err := os.Lstat(dir); err != nil {
			continue
		}
		recs, err := revlog.Read(dir)
		if err != nil {
			rc.emit("warning", l.ID, "the revision log is not readable")
			continue
		}
		s := revlog.Summarize(recs)
		if s.Commits == 0 && s.Flag == "" && len(s.Refusals) == 0 {
			continue
		}
		out = append(out, report.RevisedLeaf{NodeID: l.ID, Revisions: s.Commits, Changed: s.Changed, TestsRewritten: s.TestsRewritten, Flag: s.Flag, Refusals: s.Refusals})
	}
	return out
}
