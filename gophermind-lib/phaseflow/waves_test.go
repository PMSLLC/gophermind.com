package phaseflow

import (
	"errors"
	"testing"
)

func waveOf(t *testing.T, tasks []Task, id string) int {
	t.Helper()
	for _, tk := range tasks {
		if tk.ID == id {
			return tk.Wave
		}
	}
	t.Fatalf("task %q not found in result", id)
	return -1
}

// TestAssignWavesLinearChain: A -> B -> C (B depends on A, C depends on B)
// must get strictly increasing wave numbers.
func TestAssignWavesLinearChain(t *testing.T) {
	tasks := []Task{
		{ID: "A"},
		{ID: "B", DependsOn: []string{"A"}},
		{ID: "C", DependsOn: []string{"B"}},
	}
	out, err := AssignWaves(tasks)
	if err != nil {
		t.Fatalf("AssignWaves: %v", err)
	}
	if w := waveOf(t, out, "A"); w != 1 {
		t.Errorf("A wave = %d, want 1", w)
	}
	if w := waveOf(t, out, "B"); w != 2 {
		t.Errorf("B wave = %d, want 2", w)
	}
	if w := waveOf(t, out, "C"); w != 3 {
		t.Errorf("C wave = %d, want 3", w)
	}
}

// TestAssignWavesIndependentTasksShareAWave: two tasks with no dependencies
// on each other or anything else must land in the same wave.
func TestAssignWavesIndependentTasksShareAWave(t *testing.T) {
	tasks := []Task{{ID: "A"}, {ID: "B"}}
	out, err := AssignWaves(tasks)
	if err != nil {
		t.Fatalf("AssignWaves: %v", err)
	}
	wa, wb := waveOf(t, out, "A"), waveOf(t, out, "B")
	if wa != 1 || wb != 1 {
		t.Errorf("waves = A:%d B:%d, want both 1", wa, wb)
	}
}

// TestAssignWavesDiamond: D depends on both B and C, which both depend on A.
// D must land exactly one wave past the later of B and C (here they tie).
func TestAssignWavesDiamond(t *testing.T) {
	tasks := []Task{
		{ID: "A"},
		{ID: "B", DependsOn: []string{"A"}},
		{ID: "C", DependsOn: []string{"A"}},
		{ID: "D", DependsOn: []string{"B", "C"}},
	}
	out, err := AssignWaves(tasks)
	if err != nil {
		t.Fatalf("AssignWaves: %v", err)
	}
	wb, wc := waveOf(t, out, "B"), waveOf(t, out, "C")
	if wb != 2 || wc != 2 {
		t.Fatalf("B/C waves = %d/%d, want both 2 (fixture assumption)", wb, wc)
	}
	if wd := waveOf(t, out, "D"); wd != 3 {
		t.Errorf("D wave = %d, want 3 (one past the later of B and C)", wd)
	}
}

// TestAssignWavesDiamondUnevenDepth: D depends on B (deep) and C (shallow),
// which do not tie, so D must land one past the deeper of the two.
func TestAssignWavesDiamondUnevenDepth(t *testing.T) {
	tasks := []Task{
		{ID: "A"},
		{ID: "X", DependsOn: []string{"A"}},
		{ID: "B", DependsOn: []string{"X"}},
		{ID: "C", DependsOn: []string{"A"}},
		{ID: "D", DependsOn: []string{"B", "C"}},
	}
	out, err := AssignWaves(tasks)
	if err != nil {
		t.Fatalf("AssignWaves: %v", err)
	}
	wb := waveOf(t, out, "B")
	wd := waveOf(t, out, "D")
	if wd != wb+1 {
		t.Errorf("D wave = %d, want one past B's wave (%d)", wd, wb+1)
	}
}

// TestAssignWavesUnknownDependencyErrors: a typo'd dependency id must be a
// hard error, never a silently dropped edge that would let the dependent
// task run before its (nonexistent) input.
func TestAssignWavesUnknownDependencyErrors(t *testing.T) {
	tasks := []Task{
		{ID: "A"},
		{ID: "B", DependsOn: []string{"A-typo"}},
	}
	_, err := AssignWaves(tasks)
	if err == nil {
		t.Fatal("expected an error for an unknown dependency id, got nil")
	}
	if !contains(err.Error(), "A-typo") {
		t.Errorf("error = %q, want it to name the unknown id", err.Error())
	}
}

// TestAssignWavesCycleErrors: a cycle must error and name the ids involved,
// wrapping ErrCyclicDependency so callers can match on it.
func TestAssignWavesCycleErrors(t *testing.T) {
	tasks := []Task{
		{ID: "A", DependsOn: []string{"B"}},
		{ID: "B", DependsOn: []string{"A"}},
	}
	_, err := AssignWaves(tasks)
	if err == nil {
		t.Fatal("expected a cycle error, got nil")
	}
	if !errors.Is(err, ErrCyclicDependency) {
		t.Errorf("error = %v, want it to wrap ErrCyclicDependency", err)
	}
	if !contains(err.Error(), "A") || !contains(err.Error(), "B") {
		t.Errorf("error = %q, want it to name both A and B", err.Error())
	}
}

// TestAssignWavesEmptyListNotAnError: an empty plan is a degenerate but
// valid input.
func TestAssignWavesEmptyListNotAnError(t *testing.T) {
	out, err := AssignWaves(nil)
	if err != nil {
		t.Fatalf("AssignWaves(nil): %v", err)
	}
	if len(out) != 0 {
		t.Errorf("out = %+v, want empty", out)
	}
}

// TestAssignWavesDoesNotMutateInput: AssignWaves must be pure - the caller's
// slice and its tasks' Wave fields are untouched.
func TestAssignWavesDoesNotMutateInput(t *testing.T) {
	tasks := []Task{{ID: "A"}, {ID: "B", DependsOn: []string{"A"}}}
	_, err := AssignWaves(tasks)
	if err != nil {
		t.Fatalf("AssignWaves: %v", err)
	}
	for _, tk := range tasks {
		if tk.Wave != 0 {
			t.Errorf("input task %q Wave = %d, want untouched 0", tk.ID, tk.Wave)
		}
	}
}

// TestAssignWavesNoTaskReferencesLaterWaveOutput is the source spec's own
// acceptance test (harness-pipeline-source-spec.md section 1): over a
// generated plan, no task may depend on a task that AssignWaves placed in
// the same wave or a later one.
func TestAssignWavesNoTaskReferencesLaterWaveOutput(t *testing.T) {
	tasks := []Task{
		{ID: "contract-consumer-a"},
		{ID: "contract-consumer-b"},
		{ID: "api", DependsOn: []string{"contract-consumer-a", "contract-consumer-b"}},
		{ID: "frontend", DependsOn: []string{"api"}},
		{ID: "e2e-tests", DependsOn: []string{"frontend", "api"}},
	}
	out, err := AssignWaves(tasks)
	if err != nil {
		t.Fatalf("AssignWaves: %v", err)
	}
	byID := make(map[string]Task, len(out))
	for _, tk := range out {
		byID[tk.ID] = tk
	}
	for _, tk := range out {
		for _, dep := range tk.DependsOn {
			depWave := byID[dep].Wave
			if depWave >= tk.Wave {
				t.Errorf("task %q (wave %d) depends on %q (wave %d): dependency must be strictly earlier",
					tk.ID, tk.Wave, dep, depWave)
			}
		}
	}
}

// A contract task owns wave 0 - it is the root every later wave depends on -
// so AssignWaves must leave it there rather than reassigning it to wave 1
// along with every other dependency-free task. A caller cannot work around
// this by excluding the contract task from the input, because an unknown
// DependsOn id is a hard error.
func TestAssignWavesKeepsContractTaskAtWaveZero(t *testing.T) {
	got, err := AssignWaves([]Task{
		{ID: "contract", IsContract: true},
		{ID: "a", DependsOn: []string{"contract"}},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "loner"},
	})
	if err != nil {
		t.Fatalf("AssignWaves: %v", err)
	}
	want := map[string]int{"contract": 0, "a": 1, "b": 2, "loner": 1}
	for _, tk := range got {
		if tk.Wave != want[tk.ID] {
			t.Errorf("task %s wave = %d, want %d", tk.ID, tk.Wave, want[tk.ID])
		}
	}
}

// A contract task with its own dependencies still keeps wave 0: the marker is
// the authority on where it runs, not the edges.
func TestAssignWavesContractWaveIsNotRaisedByItsOwnDeps(t *testing.T) {
	got, err := AssignWaves([]Task{
		{ID: "prep"},
		{ID: "contract", IsContract: true, DependsOn: []string{"prep"}},
	})
	if err != nil {
		t.Fatalf("AssignWaves: %v", err)
	}
	for _, tk := range got {
		if tk.ID == "contract" && tk.Wave != 0 {
			t.Errorf("contract wave = %d, want 0", tk.Wave)
		}
	}
}
