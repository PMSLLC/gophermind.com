package serve

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func pagesMux(t *testing.T, google *GoogleAuth) http.Handler {
	t.Helper()
	d := Deps{Run: func(ctx context.Context, task string) (string, error) { return "", nil }}
	mux, err := NewMux(d, Options{Token: testBearer, Google: google})
	if err != nil {
		t.Fatal(err)
	}
	if google != nil {
		return google.Middleware(mux)
	}
	return mux
}

func TestAuthMeReportsSignedInEmail(t *testing.T) {
	a := newTestAuth(newFakeGoogle(t, goodClaims()))
	h := pagesMux(t, a)

	w := do(h, "GET", "/auth/me", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %d, want 401", w.Code)
	}
	w = do(h, "GET", "/auth/me", nil, a.sessionCookie("john@brahy.com", time.Now()))
	if w.Code != http.StatusOK {
		t.Fatalf("signed in: %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type %q", ct)
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"email":"john@brahy.com"}` {
		t.Errorf("body %q", got)
	}
	// A stale or forged cookie is not a session.
	bad := a.sessionCookie("john@brahy.com", time.Now().Add(-48*time.Hour))
	if w := do(h, "GET", "/auth/me", nil, bad); w.Code != http.StatusUnauthorized {
		t.Errorf("expired session: %d, want 401", w.Code)
	}
}

func TestPagesServeHTMLWithMarkers(t *testing.T) {
	a := newTestAuth(newFakeGoogle(t, goodClaims()))
	h := pagesMux(t, a)
	c := a.sessionCookie("john@brahy.com", time.Now())
	for path, marker := range map[string]string{
		"/":         "gm-home",
		"/app/chat": "gm-chat",
	} {
		w := do(h, "GET", path, nil, c)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, w.Code)
			continue
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("GET %s Content-Type %q", path, ct)
		}
		if !strings.Contains(w.Body.String(), marker) {
			t.Errorf("GET %s lacks marker %q", path, marker)
		}
	}
	if body := do(h, "GET", "/", nil, c).Body.String(); !strings.Contains(body, `href="/app/chat"`) ||
		!strings.Contains(body, `href="/pipeline"`) || !strings.Contains(body, "/auth/logout") {
		t.Errorf("home lacks its links: %s", body)
	}
}

func TestAuthOffRootStaysPlainLanding(t *testing.T) {
	h := pagesMux(t, nil)
	body := do(h, "GET", "/", nil).Body.String()
	if !strings.Contains(body, "GopherMind") || strings.Contains(body, "gm-home") {
		t.Errorf("auth-off root should be the plain landing: %s", body)
	}
}

func TestChatPageRedirectsWhenSignedOut(t *testing.T) {
	a := newTestAuth(newFakeGoogle(t, goodClaims()))
	h := pagesMux(t, a)
	w := do(h, "GET", "/app/chat", map[string]string{"Accept": "text/html"})
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/auth/login" {
		t.Errorf("signed out: %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestPagesNeverAssignModelOutputAsHTML(t *testing.T) {
	for _, name := range []string{"assets/chat.html", "assets/home.html"} {
		b, err := pagesAssets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, bad := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval("} {
			if strings.Contains(src, bad) {
				t.Errorf("%s uses %s", name, bad)
			}
		}
		for _, h := range []string{" onclick=", " onsubmit=", " onload=", " onchange=", " onkeydown="} {
			if strings.Contains(src, h) {
				t.Errorf("%s has inline handler %s", name, h)
			}
		}
		if strings.Contains(src, "http://") || strings.Contains(src, "https://") {
			t.Errorf("%s references an external URL", name)
		}
	}
}

func TestPipelinePageAutoConnectsOnSession(t *testing.T) {
	b, err := pipelineAssets.ReadFile("assets/pipeline.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "/auth/me") {
		t.Error("pipeline page does not probe /auth/me for a cookie session")
	}
}
