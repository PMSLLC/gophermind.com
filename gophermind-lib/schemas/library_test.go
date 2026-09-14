package schemas

import (
	"encoding/json"
	"testing"
)

func TestGetKnownSchemas(t *testing.T) {
	for _, name := range []string{"diff", "review", "plan"} {
		s, ok := Get(name)
		if !ok {
			t.Errorf("built-in schema %q missing", name)
			continue
		}
		if s["type"] != "object" {
			t.Errorf("schema %q should be an object, got %v", name, s["type"])
		}
		// Must be valid JSON-marshalable (it's handed to the API as-is).
		if _, err := json.Marshal(s); err != nil {
			t.Errorf("schema %q not marshalable: %v", name, err)
		}
		if _, ok := s["properties"]; !ok {
			t.Errorf("schema %q has no properties", name)
		}
	}
}

func TestGetUnknown(t *testing.T) {
	if _, ok := Get("nope"); ok {
		t.Error("unknown schema should not be found")
	}
}

func TestNamesSorted(t *testing.T) {
	names := Names()
	if len(names) < 3 {
		t.Fatalf("expected >=3 built-in schemas, got %d", len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("Names() not sorted: %v", names)
			break
		}
	}
}

func TestGetReturnsCopy(t *testing.T) {
	// Mutating a returned schema must not corrupt the shared library.
	s, _ := Get("plan")
	s["type"] = "mutated"
	s2, _ := Get("plan")
	if s2["type"] != "object" {
		t.Errorf("library was mutated via returned map: %v", s2["type"])
	}
}
