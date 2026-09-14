package orchestrate

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/freellm"

	"gophermind/gophermind-lib/phaseflow"
)

// A candidate list is fed to FallbackRunner, which sets Task.Model to each
// entry in turn and runs the task. The runner resolves a model by cloning
// the one configured client (see Runner.newTaskAgent), so every candidate is
// sent to that client's BaseURL with that client's key. A model id from a
// different provider is therefore not a fallback at all: it is a request for
// a model the configured endpoint has never heard of.
//
// The list must consequently never contain a model the active endpoint
// cannot serve, and it must always contain the task's own model.
func TestDefaultCandidatesForUnknownEndpointIsJustTheTasksOwnModel(t *testing.T) {
	// A BaseURL matching no free provider is the user's own endpoint, which
	// is the common case: a local llama.cpp, LM Studio, or a private vLLM.
	cands := DefaultCandidates("http://10.0.0.5:8080/v1")

	got := cands(phaseflow.Task{ID: "t1", Model: "qwen3.6-35b"})
	if len(got) != 1 || got[0] != "qwen3.6-35b" {
		t.Fatalf("got %v, want exactly [qwen3.6-35b]: a private endpoint cannot "+
			"serve another provider's models, so offering them as candidates only "+
			"spends the task's attempts on guaranteed 404s", got)
	}
}

// The concrete regression: the catalogue is full of free-provider models
// that are reachable without any key, so an unfiltered list handed every
// task ~23 foreign ids and, because it was non-empty, never fell back to
// the task's own model. Every task ran entirely on models its endpoint
// could not serve.
func TestDefaultCandidatesNeverOffersAForeignProvidersModel(t *testing.T) {
	cands := DefaultCandidates("http://127.0.0.1:1234/v1")
	got := cands(phaseflow.Task{ID: "t1", Model: "local-model"})

	for _, c := range got {
		if strings.Contains(c, "/") || strings.HasSuffix(c, ":free") {
			t.Errorf("candidate %q is a routing id from a hosted provider; it "+
				"cannot be served by the configured endpoint", c)
		}
	}
}

// A task that carries its own candidate list is authoritative: the plan
// author chose those models deliberately.
func TestDefaultCandidatesRespectsAnExplicitList(t *testing.T) {
	cands := DefaultCandidates("http://127.0.0.1:1234/v1")
	want := []string{"a", "b", "c"}
	got := cands(phaseflow.Task{ID: "t1", Model: "ignored", CandidateModels: want})
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A task with no model and no candidates has nothing to run, and saying so
// beats inventing a list.
func TestDefaultCandidatesEmptyWhenTaskHasNoModel(t *testing.T) {
	cands := DefaultCandidates("http://127.0.0.1:1234/v1")
	if got := cands(phaseflow.Task{ID: "t1"}); len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
}

// When the harness IS running on a free provider, within-provider fallback
// is real: those models share the endpoint and the key, so trying the next
// one is a genuine second chance. The task's own model still goes first.
func TestDefaultCandidatesOnAFreeProfileStaysWithinThatProvider(t *testing.T) {
	base := freeProfileBaseURL(t)
	if base == "" {
		t.Skip("no supported free provider in the registry to test against")
	}
	cands := DefaultCandidates(base)
	got := cands(phaseflow.Task{ID: "t1", Model: "some-model"})
	if len(got) == 0 {
		t.Fatal("expected at least the task's own model")
	}
	if got[0] != "some-model" {
		t.Errorf("task's own model must be tried first; got %v", got)
	}
	seen := map[string]bool{}
	for _, c := range got {
		if seen[c] {
			t.Errorf("duplicate candidate %q", c)
		}
		seen[c] = true
	}
}

// freeProfileBaseURL returns the BaseURL of some supported free provider that
// has more than one model, or "" when the registry has none.
func freeProfileBaseURL(t *testing.T) string {
	t.Helper()
	for _, c := range freellm.Compats() {
		if !c.Supported {
			continue
		}
		p, ok := freellm.Load().Lookup(c.Upstream)
		if ok && len(p.Models) > 1 {
			return c.BaseURL
		}
	}
	return ""
}
