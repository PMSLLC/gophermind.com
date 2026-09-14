package banner

import (
	"strings"
	"testing"
)

func TestAttributionLineAppearsForFreeProfile(t *testing.T) {
	out := RenderWith(Options{Profile: "free-groq", Model: "openai/gpt-oss-120b"})
	if !strings.Contains(out, "Groq") || !strings.Contains(out, "https://groq.com") {
		t.Errorf("banner missing free-provider attribution:\n%s", out)
	}
}

func TestAttributionLineAbsentForPaidProfile(t *testing.T) {
	out := RenderWith(Options{Profile: "openai", Model: "gpt-4o-mini"})
	if strings.Contains(out, "free tier") {
		t.Error("banner shows free attribution for a paid profile")
	}
}

// Options{} with no profile must render exactly as it does today, which is
// what keeps every existing banner test and caller passing.
func TestZeroOptionsAddNoAttribution(t *testing.T) {
	if strings.Contains(RenderWith(Options{}), "free tier") {
		t.Error("a zero Options value produced attribution")
	}
}
