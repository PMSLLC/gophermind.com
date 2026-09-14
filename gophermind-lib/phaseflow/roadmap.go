package phaseflow

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// PhaseNumber identifies a phase. Integer phases are planned milestone work
// (1, 2, 3); decimal phases (2.1, 2.2) are urgent insertions that sort between
// their surrounding integers. Minor == 0 denotes an integer phase.
type PhaseNumber struct {
	Major int
	Minor int
}

// String renders the phase number as written in ROADMAP.md ("2" or "2.1").
func (p PhaseNumber) String() string {
	if p.Minor == 0 {
		return strconv.Itoa(p.Major)
	}
	return fmt.Sprintf("%d.%d", p.Major, p.Minor)
}

// Less reports whether p sorts before q in execution order.
func (p PhaseNumber) Less(q PhaseNumber) bool {
	if p.Major != q.Major {
		return p.Major < q.Major
	}
	return p.Minor < q.Minor
}

// ParsePhaseNumber parses "2" or "2.1" into a PhaseNumber.
func ParsePhaseNumber(s string) (PhaseNumber, error) {
	s = strings.TrimSpace(s)
	major, minor, found := strings.Cut(s, ".")
	maj, err := strconv.Atoi(major)
	if err != nil || maj < 0 {
		return PhaseNumber{}, fmt.Errorf("invalid phase number %q", s)
	}
	if !found {
		return PhaseNumber{Major: maj}, nil
	}
	min, err := strconv.Atoi(minor)
	if err != nil || min < 0 {
		return PhaseNumber{}, fmt.Errorf("invalid phase number %q", s)
	}
	return PhaseNumber{Major: maj, Minor: min}, nil
}

// Plan is a single unit of work within a phase, tracked as a checkbox in the
// phase's Plans list. ID has the form "{phase}-{plan}", e.g. "01-02" or
// "02.1-01".
type Plan struct {
	ID          string
	Description string
	Done        bool
}

// Phase is one scoped, independently shippable unit of the roadmap.
type Phase struct {
	Number      PhaseNumber
	Name        string
	Description string // one-line summary from the phase list
	Goal        string // from the detail section's **Goal**
	DependsOn   string // from **Depends on**
	Inserted    bool   // decimal phase marked INSERTED
	Done        bool   // summary checkbox state
	Plans       []Plan
}

// Roadmap is the parsed view of .planning/ROADMAP.md.
type Roadmap struct {
	Title  string
	Phases []Phase
}

var (
	// "- [ ] **Phase 2.1: Name** - description"
	rePhaseSummary = regexp.MustCompile(`^\s*-\s*\[( |x|X)\]\s*\*\*Phase\s+([0-9]+(?:\.[0-9]+)?)\s*:\s*(.+?)\*\*\s*(?:-\s*(.*))?$`)
	// "### Phase 2.1: Name (INSERTED)" — heading depth 2-4
	rePhaseHeading = regexp.MustCompile(`^#{2,4}\s*Phase\s+([0-9]+(?:\.[0-9]+)?)\s*:\s*(.+?)\s*$`)
	// "- [ ] 02-01: description" plan checkbox
	rePlan = regexp.MustCompile(`^\s*-\s*\[( |x|X)\]\s*([0-9]+(?:\.[0-9]+)?-[0-9]+)\s*:\s*(.*)$`)
	// "**Goal**: ..." / "**Depends on**: ..."
	reGoal      = regexp.MustCompile(`(?i)^\s*\*\*Goal\*\*\s*:\s*(.*)$`)
	reDependsOn = regexp.MustCompile(`(?i)^\s*\*\*Depends on\*\*\s*:\s*(.*)$`)
	reTitle     = regexp.MustCompile(`^#\s*Roadmap\s*:\s*(.*)$`)
)

// LoadRoadmap reads and parses .planning/ROADMAP.md for a project root.
func LoadRoadmap(root string) (*Roadmap, error) {
	data, err := os.ReadFile(RoadmapPath(root))
	if err != nil {
		return nil, err
	}
	return ParseRoadmap(string(data))
}

// ParseRoadmap parses ROADMAP.md content. It reads the phase list for summary
// checkboxes and one-line descriptions, then the "### Phase N" detail sections
// for goal, dependencies, and the plan checkboxes. Detail data is merged onto
// the matching summary phase; a phase that appears only in detail form (an
// orphan) is still captured.
func ParseRoadmap(content string) (*Roadmap, error) {
	rm := &Roadmap{}
	// byNum maps a phase number to its index in rm.Phases. Indices (not
	// pointers) are used because rm.Phases grows via append, which can move the
	// backing array and invalidate any pointers held into it.
	byNum := map[string]int{}
	phase := func(num PhaseNumber, name string) int {
		key := num.String()
		if idx, ok := byNum[key]; ok {
			if rm.Phases[idx].Name == "" {
				rm.Phases[idx].Name = name
			}
			return idx
		}
		rm.Phases = append(rm.Phases, Phase{Number: num, Name: name})
		idx := len(rm.Phases) - 1
		byNum[key] = idx
		return idx
	}

	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	cur := -1 // index of the phase whose detail section we are inside, or -1
	for sc.Scan() {
		line := sc.Text()

		if rm.Title == "" {
			if m := reTitle.FindStringSubmatch(line); m != nil {
				rm.Title = strings.TrimSpace(m[1])
				continue
			}
		}

		if m := rePhaseSummary.FindStringSubmatch(line); m != nil {
			num, err := ParsePhaseNumber(m[2])
			if err != nil {
				return nil, err
			}
			name, inserted := stripInserted(strings.TrimSpace(m[3]))
			idx := phase(num, name)
			rm.Phases[idx].Done = strings.EqualFold(m[1], "x")
			rm.Phases[idx].Description = strings.TrimSpace(m[4])
			if inserted {
				rm.Phases[idx].Inserted = true
			}
			continue
		}

		if m := rePhaseHeading.FindStringSubmatch(line); m != nil {
			num, err := ParsePhaseNumber(m[1])
			if err != nil {
				return nil, err
			}
			name, inserted := stripInserted(strings.TrimSpace(m[2]))
			cur = phase(num, name)
			if inserted {
				rm.Phases[cur].Inserted = true
			}
			continue
		}

		if cur >= 0 {
			if m := reGoal.FindStringSubmatch(line); m != nil {
				rm.Phases[cur].Goal = strings.TrimSpace(m[1])
				continue
			}
			if m := reDependsOn.FindStringSubmatch(line); m != nil {
				rm.Phases[cur].DependsOn = strings.TrimSpace(m[1])
				continue
			}
			if m := rePlan.FindStringSubmatch(line); m != nil {
				rm.Phases[cur].Plans = append(rm.Phases[cur].Plans, Plan{
					ID:          m[2],
					Description: strings.TrimSpace(m[3]),
					Done:        strings.EqualFold(m[1], "x"),
				})
				continue
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return rm, nil
}

// stripInserted removes a trailing "(INSERTED)" marker (any case) from a phase
// name and reports whether it was present.
func stripInserted(name string) (string, bool) {
	if !strings.Contains(strings.ToUpper(name), "(INSERTED)") {
		return name, false
	}
	cleaned := strings.NewReplacer("(INSERTED)", "", "(Inserted)", "", "(inserted)", "").Replace(name)
	return strings.TrimSpace(cleaned), true
}

// Phase returns the phase with the given number, or nil if absent.
func (r *Roadmap) Phase(num PhaseNumber) *Phase {
	for i := range r.Phases {
		if r.Phases[i].Number == num {
			return &r.Phases[i]
		}
	}
	return nil
}

// NextPhase returns the lowest-numbered phase that is not yet Done, or nil when
// every phase is complete. Phases are consulted in execution (numeric) order.
func (r *Roadmap) NextPhase() *Phase {
	var next *Phase
	for i := range r.Phases {
		p := &r.Phases[i]
		if p.Done {
			continue
		}
		if next == nil || p.Number.Less(next.Number) {
			next = p
		}
	}
	return next
}

// TotalPlans returns the number of plans across all phases and how many are done.
func (r *Roadmap) TotalPlans() (done, total int) {
	for i := range r.Phases {
		for _, pl := range r.Phases[i].Plans {
			total++
			if pl.Done {
				done++
			}
		}
	}
	return done, total
}

// Percent returns overall completion as a whole-number percentage of plans done.
// With no plans it reports 0.
func (r *Roadmap) Percent() int {
	done, total := r.TotalPlans()
	if total == 0 {
		return 0
	}
	return done * 100 / total
}
