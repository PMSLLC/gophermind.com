package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// BriefProgress is how far the step a run is in has got. Total is 0 when the
// size of the step is not known (the page then shows a plain "working" bar).
type BriefProgress struct {
	Step    string `json:"step"`
	Label   string `json:"label"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Percent int    `json:"percent"`
	Calls   int    `json:"calls"`
}

// planStages are the planner's steps in order, as the log names them.
var planStages = []string{"load", "clarify", "confirm", "contract", "decompose", "enrich", "coverage", "approve", "testwriter"}

// currentPlanStage is the last planner step the log shows started but not
// done, or "".
func currentPlanStage(logText string) string {
	open := ""
	for _, line := range strings.Split(logText, "\n") {
		line = strings.TrimSpace(line)
		for _, s := range planStages {
			switch line {
			case s + ": started":
				open = s
			case s + ": done":
				if open == s {
					open = ""
				}
			}
		}
	}
	return open
}

// jsonKeys counts the keys of the object at path[key] in a run-folder file.
func jsonKeys(file, key string) int {
	b, err := os.ReadFile(file)
	if err != nil {
		return 0
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(b, &doc) != nil {
		return 0
	}
	var inner map[string]json.RawMessage
	if json.Unmarshal(doc[key], &inner) != nil {
		var list []json.RawMessage
		if json.Unmarshal(doc[key], &list) != nil {
			return 0
		}
		return len(list)
	}
	return len(inner)
}

// progressOf reads the step the run is in. dir is the run folder.
func progressOf(dir string, st *BriefState, logText string) *BriefProgress {
	calls := func(stage string) int {
		for _, g := range st.Stages {
			if g.Stage == stage || strings.HasPrefix(g.Stage, stage+":") {
				return g.Calls
			}
		}
		return 0
	}
	p := &BriefProgress{}
	if st.Run.Phase == phaseRunning {
		p.Step, p.Label = "build", "nodes verified"
		p.Done, p.Total = st.Run.Counts.ByStatus["verified"], st.Run.Counts.Nodes
		p.Calls = calls("implement")
	} else {
		stage := currentPlanStage(logText)
		if stage == "" {
			return nil
		}
		p.Step = stage
		total := jsonKeys(filepath.Join(dir, "contracts.json"), "components")
		switch stage {
		case "decompose":
			p.Label, p.Done, p.Total = "components broken into functions", jsonKeys(filepath.Join(dir, "_state", "decomposed.json"), "components"), total
		case "enrich":
			p.Label, p.Done, p.Total = "components written up", jsonKeys(filepath.Join(dir, "_state", "enriched.json"), "components"), total
		case "testwriter":
			p.Label, p.Calls = "tests being written", calls("testwrite")
		default:
			p.Label, p.Calls = "working", calls(stage)
		}
	}
	if p.Total > 0 {
		if p.Done > p.Total {
			p.Done = p.Total
		}
		p.Percent = p.Done * 100 / p.Total
	}
	return p
}
