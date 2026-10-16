package planner

import (
	"reflect"
	"testing"
)

func TestOpenQuestionNodesListsOnlyNodesWithOpenQuestions(t *testing.T) {
	dec := decomposed{Components: map[string][]map[string]any{
		"c": {
			{"id": "fn-a", "open_questions": []any{}},
			{"id": "fn-b", "open_questions": []any{"Which store?"}},
			{"id": "fn-c"},
		},
	}}
	if got := openQuestionNodes(dec); !reflect.DeepEqual(got, []string{"fn-b"}) {
		t.Errorf("got %v", got)
	}
}

func TestCitedByFunctionsInvertsDecisionIDs(t *testing.T) {
	dec := decomposed{Components: map[string][]map[string]any{
		"c": {
			{"id": "fn-b", "decision_ids": []any{"q1", "q2"}},
			{"id": "fn-a", "decision_ids": []any{"q2"}},
		},
	}}
	got := citedByFunctions(dec)
	if !reflect.DeepEqual(got["q1"], []string{"fn-b"}) || !reflect.DeepEqual(got["q2"], []string{"fn-a", "fn-b"}) {
		t.Errorf("got %v", got)
	}
}

func TestApprovalHashCoversTheConfirmedUnderstandingAndEnrichedFields(t *testing.T) {
	for _, want := range []string{stateUnderstanding, stateEnriched} {
		found := false
		for _, f := range hashedFiles {
			if f == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s must be hashed so changing it invalidates the approval", want)
		}
	}
}
