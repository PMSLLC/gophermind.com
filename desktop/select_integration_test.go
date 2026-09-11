package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gophermind/internal/freellm"
	"gophermind/internal/modelcat"
	"gophermind/internal/prompt"
	"gophermind/internal/serve"
)

// startTestServer wires the same pieces startEmbeddedServer does (tool
// registry, prompt, token, mux, loopback listener), but with holder already
// populated: this test needs to control the active client's profile label
// directly (free-ovhcloud, with model usage seeded over its threshold),
// which startEmbeddedServer's own env-driven resolution has no seam for.
func startTestServer(t *testing.T, holder *clientHolder) *embeddedServer {
	t.Helper()
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	reg := newToolRegistry(cfg)
	pb, err := prompt.NewBuilder()
	if err != nil {
		t.Fatalf("prompt.NewBuilder: %v", err)
	}
	basePrompt := pb.Build()

	token, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}

	mux, err := serve.NewMux(newServeDeps(holder.Get, holder.Profile, holder.Set, reg, cfg, basePrompt), serve.Options{Token: token})
	if err != nil {
		t.Fatalf("serve.NewMux: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	srv := &embeddedServer{
		BaseURL: "http://" + ln.Addr().String(),
		Token:   token,
		cancel:  cancel,
		done:    make(chan error, 1),
	}
	go func() { srv.done <- serve.Serve(ctx, ln, withCORS(mux)) }()
	t.Cleanup(func() {
		if err := srv.Shutdown(); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	return srv
}

// seedNearCapacityModel writes an odometer whose "free-ovhcloud/gpt-oss-120b"
// usage is well past its published 2 RPM quota, and a settings file with
// cycling as requested and gpt-oss-20b (same profile, same quota by
// construction, since modelcat draws every model's quota from the
// profile's default model) preferred first.
func seedNearCapacityModel(t *testing.T, odometerPath, settingsPath string, cycleOnCapacity bool) {
	t.Helper()
	o := &freellm.Odometer{}
	if err := o.Add(odometerPath, freellm.Event{
		TS: time.Now(), Profile: "free-ovhcloud", Model: "gpt-oss-120b", Requests: 5,
	}); err != nil {
		t.Fatalf("seed odometer: %v", err)
	}
	s := modelcat.DefaultSettings()
	s.CycleOnCapacity = cycleOnCapacity
	s.Order = []string{"free-ovhcloud/gpt-oss-20b", "free-ovhcloud/gpt-oss-120b"}
	s.WhenAllFull = "stay"
	if err := modelcat.SaveSettings(settingsPath, s); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
}

// runOneTurnAndCollectFrames creates a session against srv and runs one
// streamed turn, returning every SSE frame observed up to and including
// "done".
func runOneTurnAndCollectFrames(t *testing.T, srv *embeddedServer) []sseFrame {
	t.Helper()
	createReq, _ := http.NewRequest(http.MethodPost, srv.BaseURL+"/session", nil)
	createReq.Header.Set("Authorization", "Bearer "+srv.Token)
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("POST /session: %v", err)
	}
	var created struct {
		ID string `json:"id"`
	}
	decErr := json.NewDecoder(createResp.Body).Decode(&created)
	createResp.Body.Close()
	if decErr != nil {
		t.Fatalf("decode /session response: %v", decErr)
	}
	if created.ID == "" {
		t.Fatal("POST /session returned an empty id")
	}

	streamReq, _ := http.NewRequest(http.MethodPost, srv.BaseURL+"/session/"+created.ID+"/stream", strings.NewReader("say hello"))
	streamReq.Header.Set("Authorization", "Bearer "+srv.Token)
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatalf("POST /session/{id}/stream: %v", err)
	}
	if streamResp.StatusCode != http.StatusOK {
		streamResp.Body.Close()
		t.Fatalf("POST /session/{id}/stream: want 200, got %d", streamResp.StatusCode)
	}

	frames := make(chan sseFrame, 32)
	go readSSE(streamResp, frames)

	var out []sseFrame
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				return out
			}
			out = append(out, f)
			if f.event == "done" {
				return out
			}
		case <-time.After(15 * time.Second):
			t.Fatal("timed out waiting for the turn to finish")
		}
	}
}

// TestModelSwitchedFrameOnCycling proves the turn-start wiring in
// applyModelPolicy (desktop/deps.go): with cycling on and the active
// model's usage seeded past its capacity threshold, a session turn emits a
// model-switched frame naming the preferred, non-full model in the same
// profile before the turn's own output. With cycling off, no such frame
// arrives even though the same over-capacity usage is present.
func TestModelSwitchedFrameOnCycling(t *testing.T) {
	for _, tc := range []struct {
		name            string
		cycleOnCapacity bool
		wantSwitch      bool
	}{
		{"cycling on switches", true, true},
		{"cycling off never switches", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := &scriptedLLM{turns: [][]byte{finalResp("hello back")}}
			llmSrv := httptest.NewServer(script.handler())
			defer llmSrv.Close()

			dir := t.TempDir()
			t.Setenv("GOPHERMIND_BASE_URL", llmSrv.URL)
			t.Setenv("GOPHERMIND_MODEL", "gpt-oss-120b")
			t.Setenv("GOPHERMIND_ROOT", t.TempDir())
			t.Setenv("GOPHERMIND_APPROVAL", "auto")
			odometerPath := dir + "/odometer.json"
			settingsPath := dir + "/model-settings.json"
			t.Setenv("GOPHERMIND_ODOMETER", odometerPath)
			t.Setenv("GOPHERMIND_MODEL_SETTINGS", settingsPath)

			seedNearCapacityModel(t, odometerPath, settingsPath, tc.cycleOnCapacity)

			cfg, err := loadConfig()
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			client, err := newLLMClient(ctx, cfg)
			cancel()
			if err != nil {
				t.Fatalf("newLLMClient: %v", err)
			}

			// The client points at the local llmSrv stub, never a real
			// provider; the profile label is what applyModelPolicy actually
			// reads to look the current model up in the catalogue, and is
			// set here directly rather than through the real fallback path
			// (which would dial free-ovhcloud's real endpoint).
			holder := &clientHolder{}
			holder.Set(client, "free-ovhcloud")

			srv := startTestServer(t, holder)

			frames := runOneTurnAndCollectFrames(t, srv)

			var switchFrame *sseFrame
			for i := range frames {
				if frames[i].event == "model-switched" {
					switchFrame = &frames[i]
					break
				}
			}

			if tc.wantSwitch {
				if switchFrame == nil {
					t.Fatal("expected a model-switched frame, got none")
				}
				var payload struct {
					Profile string `json:"profile"`
					Model   string `json:"model"`
					Reason  string `json:"reason"`
				}
				if err := json.Unmarshal([]byte(switchFrame.data), &payload); err != nil {
					t.Fatalf("model-switched frame not JSON: %v (data=%s)", err, switchFrame.data)
				}
				if payload.Profile != "free-ovhcloud" || payload.Model != "gpt-oss-20b" {
					t.Fatalf("model-switched frame = %+v, want free-ovhcloud/gpt-oss-20b", payload)
				}
				if payload.Reason == "" {
					t.Error("model-switched frame carried an empty reason")
				}
			} else if switchFrame != nil {
				t.Fatalf("cycling off: got an unexpected model-switched frame: %+v", *switchFrame)
			}
		})
	}
}
