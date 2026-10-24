package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Every embedded page leaves the handler with the stylesheet and the rail in
// place, no marker left over, and the rail marks its own entry as current.
func TestPagesCarryTheSharedStylesheetAndRail(t *testing.T) {
	for name, nav := range map[string]string{
		"assets/home.html":      "",
		"assets/briefs.html":    `<a href="/app/briefs" aria-current="page">Runs</a>`,
		"assets/brief.html":     `<a href="/app/briefs" aria-current="page">Runs</a>`,
		"assets/brief_new.html": `<a href="/app/briefs/new" aria-current="page">Upload brief</a>`,
		"assets/chat.html":      `<a href="/app/chat" aria-current="page">Chat</a>`,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			pageHandler(name)(w, httptest.NewRequest(http.MethodGet, "/", nil))
			body := w.Body.String()
			if w.Code != http.StatusOK || strings.Contains(body, pageCSSMarker) || strings.Contains(body, pageRailMarker) {
				t.Fatalf("code %d, a marker is left in the page", w.Code)
			}
			for _, want := range []string{"--accent:", `class="rail"`, "Content-Security-Policy", `action="/auth/logout"`, nav} {
				if !strings.Contains(body, want) {
					t.Errorf("page lacks %q", want)
				}
			}
			if nav == "" && strings.Contains(body, `aria-current="page"`) {
				t.Error("the home page marks a rail entry as current")
			}
			if strings.Contains(body, "innerHTML") {
				t.Error("a page sets innerHTML")
			}
		})
	}
}
