package planner

import (
	"strings"
	"testing"
)

// The rule a contract type declaration is refused for (typedecl_shape.go) is
// told to the model up front, in the item schema every contract prompt embeds,
// so a Status enum with its constants no longer costs a round to learn it.
func TestContractItemSchemaStatesTheTypeDeclarationShape(t *testing.T) {
	got, err := contractItemSchemas("types")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type declarations and methods only", "no const, var, import or plain function", "named type"} {
		if !strings.Contains(got, want) {
			t.Errorf("type item schema lacks %q:\n%s", want, got)
		}
	}
}
