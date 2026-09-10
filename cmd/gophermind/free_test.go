package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFreeListShowsNoKeyProvidersFirst(t *testing.T) {
	var buf bytes.Buffer
	if code := runFree([]string{"list"}, &buf); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	out := buf.String()
	for _, want := range []string{"free-ovhcloud", "free-groq", "no key"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
	// A no-key provider must appear before a key-required one.
	if strings.Index(out, "free-ovhcloud") > strings.Index(out, "free-groq") {
		t.Error("key-required provider listed before a no-key one")
	}
}

func TestFreeListMarksTerms(t *testing.T) {
	var buf bytes.Buffer
	runFree([]string{"list"}, &buf)
	out := buf.String()
	if !strings.Contains(out, "non-commercial") {
		t.Errorf("list does not flag Cohere's non-commercial terms:\n%s", out)
	}
}

func TestFreeShowPrintsExportLines(t *testing.T) {
	var buf bytes.Buffer
	if code := runFree([]string{"show", "free-groq"}, &buf); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	out := buf.String()
	for _, want := range []string{
		"https://groq.com",
		"openai/gpt-oss-120b",
		"GOPHERMIND_PROFILE_FREE_GROQ_API_KEY",
		"--profile free-groq",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestFreeShowNoKeyProviderOmitsKeyLine(t *testing.T) {
	var buf bytes.Buffer
	runFree([]string{"show", "free-ovhcloud"}, &buf)
	if strings.Contains(buf.String(), "API_KEY") {
		t.Errorf("show told the user to set a key for a no-key provider:\n%s", buf.String())
	}
}

func TestFreeShowUnknownProfileErrors(t *testing.T) {
	var buf bytes.Buffer
	if code := runFree([]string{"show", "free-nope"}, &buf); code == 0 {
		t.Error("expected a nonzero exit for an unknown profile")
	}
}

func TestFreeUsageRunsWithNoOdometer(t *testing.T) {
	t.Setenv("GOPHERMIND_ODOMETER", t.TempDir()+"/odo.json")
	var buf bytes.Buffer
	if code := runFree([]string{"usage"}, &buf); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(buf.String(), "0") {
		t.Errorf("usage output does not show a zero reading:\n%s", buf.String())
	}
}

func TestFreeUnknownSubcommandErrors(t *testing.T) {
	var buf bytes.Buffer
	if code := runFree([]string{"wat"}, &buf); code == 0 {
		t.Error("expected a nonzero exit for an unknown subcommand")
	}
}
