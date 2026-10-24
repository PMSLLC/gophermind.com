package serve

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"gophermind/gophermind-lib/build/report"
)

// Wave check results (BriefWave.Check).
const (
	checkPass      = "pass"      // every leaf verified and the run went on (or ended verified)
	checkFail      = "fail"      // a leaf of the wave failed or escalated, or the run stopped on the wave's integration check
	checkRepairing = "repairing" // the wave's integration check failed and leaves were reopened
	checkRunning   = "running"   // leaves of the wave are being built
	checkPending   = "pending"
)

// BriefWave is one wave of leaves and the result of its integration check
// (go build, go vet and the tests, run when the wave's leaves are verified).
// The executor does not keep a log of each check, so Check is read from what
// it does keep: leaf statuses, _state/leaf-results.json and the run's status.
type BriefWave struct {
	Wave          int      `json:"wave"`
	Leaves        []string `json:"leaves"`
	Verified      int      `json:"verified"`
	Check         string   `json:"check"`
	FailingLeaves []string `json:"failing_leaves"`
}

// BriefRepair is a leaf reopened because its wave's integration check failed
// and which is not verified again yet. Attempts is how many model attempts the
// leaf has made so far; Reason is the executor's class ("integration: build").
type BriefRepair struct {
	Leaf     string `json:"leaf"`
	Wave     int    `json:"wave"`
	Attempts int    `json:"attempts"`
	Reason   string `json:"reason"`
	Status   string `json:"status"`
}

// BriefRepairs is the run's repair activity. Total is the report's count of
// leaf reopenings once the run has ended (zero before), Open are the leaves
// being repaired or that ended unrepaired.
type BriefRepairs struct {
	Total int           `json:"total"`
	Open  []BriefRepair `json:"open"`
}

type leafResultDisk struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func readLeafResults(dir string) map[string]leafResultDisk {
	out := map[string]leafResultDisk{}
	if b, err := readSmallFile(filepath.Join(dir, "_state", "leaf-results.json"), 4<<20); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// stopsOnIntegration reports whether a stop reason is the wave checks' own.
func stopsOnIntegration(reason string) bool {
	for _, k := range []string{"repair", "integration", "stray_write", "unattributable"} {
		if strings.Contains(reason, k) {
			return true
		}
	}
	return false
}

// waveChecks derives the per-wave check results and the repairs from the
// leaves, the leaf-results file and the report.
func waveChecks(dir string, run BriefRun, nodes []BriefNode) ([]BriefWave, BriefRepairs) {
	results := readLeafResults(dir)
	byWave := map[int][]BriefNode{}
	for _, n := range nodes {
		if n.Kind == "function" {
			byWave[n.Wave] = append(byWave[n.Wave], n)
		}
	}
	nums := make([]int, 0, len(byWave))
	for w := range byWave {
		nums = append(nums, w)
	}
	sort.Ints(nums)

	waves := []BriefWave{}
	repairs := BriefRepairs{Open: []BriefRepair{}}
	started := func(w int) bool { // any leaf past pending
		for _, n := range byWave[w] {
			if n.RawStatus != "pending" && n.RawStatus != "" {
				return true
			}
		}
		return false
	}
	for i, w := range nums {
		bw := BriefWave{Wave: w, Leaves: []string{}, FailingLeaves: []string{}}
		var failed, reopened, active []string
		for _, n := range byWave[w] {
			bw.Leaves = append(bw.Leaves, n.ID)
			res := results[n.ID]
			switch n.RawStatus {
			case "verified":
				bw.Verified++
			case "failed", "escalated":
				failed = append(failed, n.ID)
			case "needs_revision":
				reopened = append(reopened, n.ID)
			case "claimed", "in_progress", "ready":
				active = append(active, n.ID)
			}
			if n.RawStatus != "verified" && strings.HasPrefix(res.Reason, "integration:") {
				repairs.Open = append(repairs.Open, BriefRepair{Leaf: n.ID, Wave: w, Attempts: len(n.Attempts),
					Reason: res.Reason, Status: n.RawStatus})
				if n.RawStatus != "failed" && n.RawStatus != "escalated" && n.RawStatus != "needs_revision" {
					reopened = append(reopened, n.ID)
				}
			}
		}
		laterStarted := false
		for _, later := range nums[i+1:] {
			laterStarted = laterStarted || started(later)
		}
		switch {
		case len(failed) > 0:
			bw.Check, bw.FailingLeaves = checkFail, failed
		case len(reopened) > 0:
			bw.Check, bw.FailingLeaves = checkRepairing, reopened
		case bw.Verified == len(bw.Leaves) && (laterStarted || run.Status == "verified"):
			bw.Check = checkPass
		case bw.Verified == len(bw.Leaves) && stopsOnIntegration(run.StopReason) &&
			(run.Status == "failed" || run.Status == "escalated"):
			bw.Check = checkFail
		case len(active) > 0 || bw.Verified > 0:
			bw.Check = checkRunning
		default:
			bw.Check = checkPending
		}
		waves = append(waves, bw)
	}
	if rep, err := report.Read(dir); err == nil {
		repairs.Total = rep.Repairs
	}
	return waves, repairs
}
