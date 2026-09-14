package llm

import "testing"

// CloneForModel exists so a concurrent task's recorded attempt names the model
// that actually ran. Carrying the parent's Fallbacks into the clone defeats
// that: chain() tries Model first and then every fallback, so a clone made for
// model A can have its request served by the parent's fallback model B while
// the attempt history still says A. That is the same "record of intentions
// rather than of what ran" the clone was introduced to stop, arriving through
// a second door.
//
// Per-candidate fallback belongs one level up, in phaseflow.FallbackRunner,
// which records an Attempt for each model it tries. Two independent layers of
// fallback cannot both be accounted for.
func TestCloneForModelDoesNotInheritFallbacks(t *testing.T) {
	parent := &Client{
		BaseURL:   "http://example.invalid/v1",
		Model:     "primary",
		Fallbacks: []string{"backup-1", "backup-2"},
	}

	clone := parent.CloneForModel("candidate-a")

	if len(clone.Fallbacks) != 0 {
		t.Errorf("clone inherited Fallbacks %v: a request for %q could be served "+
			"by one of them while the attempt records %q",
			clone.Fallbacks, "candidate-a", "candidate-a")
	}
	if got := clone.chain(); len(got) != 1 || got[0] != "candidate-a" {
		t.Errorf("clone.chain() = %v, want exactly [candidate-a]", got)
	}
	// The parent is untouched: nothing about cloning should disarm the
	// fallback behaviour a directly-configured client was given.
	if len(parent.Fallbacks) != 2 {
		t.Errorf("parent.Fallbacks = %v, want the original two", parent.Fallbacks)
	}
	if got := parent.chain(); len(got) != 3 {
		t.Errorf("parent.chain() = %v, want primary plus its two fallbacks", got)
	}
}
