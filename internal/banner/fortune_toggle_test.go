package banner

import (
	"strings"
	"testing"

	"gophermind/internal/version"
)

func TestRenderWithFortuneOff(t *testing.T) {
	withFortune := RenderWith(Options{Fortune: true})
	noFortune := RenderWith(Options{Fortune: false})

	// Both keep the banner core (version line).
	if !strings.Contains(noFortune, version.String()) {
		t.Errorf("no-fortune banner missing version line")
	}
	// The fortune adds a trailing block, so omitting it must shorten the output.
	// (fortune.Random always returns a non-empty embedded fortune.)
	if len(noFortune) >= len(withFortune) {
		t.Errorf("fortune-off render (%d) should be shorter than fortune-on (%d)", len(noFortune), len(withFortune))
	}
}

func TestRenderDefaultsToFortuneOn(t *testing.T) {
	if len(Render()) < len(RenderWith(Options{Fortune: false})) {
		t.Error("Render() should include the fortune by default")
	}
}
