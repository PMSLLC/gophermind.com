package planner

import (
	"strings"
	"testing"
)

// A model wrote the first root test as an object and the others as JSON text
// inside a string: "{\"requirement\":\"A2\",...}". The reply was refused three
// times for the wrong type.
func TestCoverageReplyAcceptsRootTestsWrittenAsJSONText(t *testing.T) {
	reqs := []Requirement{{ID: "A1"}, {ID: "A2"}}
	text := `{"map": [], "root_tests": [
	  {"requirement": "A1", "name": "build", "given": "root", "expect": "ok", "command": "go build ./..."},
	  "{\"requirement\":\"A2\",\"name\":\"vet\",\"given\":\"root\",\"expect\":\"ok\",\"command\":\"go vet ./...\"}"]}`
	r, err := ParseCoverageReply(text, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.RootTests) != 2 || r.RootTests[1].Command != "go vet ./..." {
		t.Errorf("root tests = %+v", r.RootTests)
	}
}

func TestCoverageReplyStillRefusesRootTestsThatAreNotObjects(t *testing.T) {
	reqs := []Requirement{{ID: "A1"}}
	for _, bad := range []string{`"just a sentence"`, `"[1,2]"`, `42`} {
		_, err := ParseCoverageReply(`{"map": [], "root_tests": [`+bad+`]}`, reqs)
		if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
			t.Errorf("%s: err = %v, want the wrong-type refusal", bad, err)
		}
	}
}
