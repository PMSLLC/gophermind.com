package tools

import (
	"strings"
	"testing"
)

const sampleSpec = `{
  "openapi": "3.0.0",
  "info": {"title": "Pet API", "version": "1.0"},
  "servers": [{"url": "https://api.example.com/v1"}],
  "paths": {
    "/pets": {
      "get": {"operationId": "listPets", "summary": "List all pets"},
      "post": {"operationId": "createPet", "summary": "Create a pet"}
    },
    "/pets/{id}": {
      "get": {
        "operationId": "getPet",
        "summary": "Get a pet",
        "parameters": [{"name": "id", "in": "path", "required": true}]
      }
    }
  }
}`

func TestOpenAPIListsOperations(t *testing.T) {
	dir := writeFileT(t, t.TempDir(), "spec.json", sampleSpec)
	out, err := run(t, OpenAPIOps(dir), `{"spec":"spec.json"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"GET", "POST", "/pets", "/pets/{id}", "listPets", "createPet", "getPet"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// The base server URL should be surfaced so the model knows where to call.
	if !strings.Contains(out, "https://api.example.com/v1") {
		t.Errorf("server URL missing:\n%s", out)
	}
}

func TestOpenAPIShowsPathParams(t *testing.T) {
	dir := writeFileT(t, t.TempDir(), "spec.json", sampleSpec)
	out, err := run(t, OpenAPIOps(dir), `{"spec":"spec.json"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "id") {
		t.Errorf("path parameter 'id' should be listed:\n%s", out)
	}
}

func TestOpenAPIBadSpec(t *testing.T) {
	dir := writeFileT(t, t.TempDir(), "spec.json", `{not json`)
	if _, err := run(t, OpenAPIOps(dir), `{"spec":"spec.json"}`); err == nil {
		t.Error("malformed spec should error")
	}
}

func TestOpenAPIContainsPath(t *testing.T) {
	dir := writeFileT(t, t.TempDir(), "spec.json", sampleSpec)
	if _, err := run(t, OpenAPIOps(dir), `{"spec":"../../etc/spec.json"}`); err == nil {
		t.Error("spec path escaping the root should be rejected")
	}
}
