package agent

import (
	"strings"
	"testing"
)

func TestExportRedactsWhenEnabled(t *testing.T) {
	a := New(nil, nil, 1, nil, nil)
	seed := `{"role":"system","content":"sys"}` + "\n" +
		`{"role":"user","content":"my key is ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"}` + "\n"
	if err := a.LoadHistory(strings.NewReader(seed)); err != nil {
		t.Fatal(err)
	}

	// Without redaction the secret is present.
	var plain strings.Builder
	if err := a.ExportJSONL(&plain); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain.String(), "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") {
		t.Fatal("baseline export should contain the secret")
	}

	// With redaction the secret is scrubbed.
	a.SetRedactTranscript(true)
	var scrubbed strings.Builder
	if err := a.ExportJSONL(&scrubbed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(scrubbed.String(), "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") {
		t.Errorf("secret not redacted in export: %s", scrubbed.String())
	}
	if !strings.Contains(scrubbed.String(), "REDACTED") {
		t.Errorf("expected placeholder in redacted export: %s", scrubbed.String())
	}
}
