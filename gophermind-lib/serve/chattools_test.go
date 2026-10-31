package serve

import (
	"context"
	"net/http"
	"testing"

	"gophermind/gophermind-lib/tools"
)

func TestSessionStreamCarriesChatTools(t *testing.T) {
	for _, tc := range []struct {
		name    string
		briefs  bool
		wantNil bool
	}{
		{"briefs on", true, false},
		{"briefs off", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []tools.Tool
			called := false
			e := newBriefEnv(t, func(o *Options, d *Deps) {
				if !tc.briefs {
					o.BriefsDir = ""
				}
				d.SessionTurn = func(ctx context.Context, id, task string, emit func(event, data string) error) error {
					called = true
					got = ChatToolsFrom(ctx)
					return nil
				}
			})
			w := e.post("/session/abc/stream", "hi", map[string]string{"Authorization": "Bearer " + testBearer})
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
			if !called {
				t.Fatal("turn not called")
			}
			if tc.wantNil && got != nil {
				t.Errorf("got %v, want nil", got)
			}
			if !tc.wantNil && got == nil {
				t.Error("got nil, want non-nil slice")
			}
		})
	}
}
