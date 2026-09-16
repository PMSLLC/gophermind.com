//go:build e2e

package main

// This file builds a real serve.Deps/serve.NewMux HTTP server in-process,
// for e2e_remote_test.go's remote-mode tests to run behind a real
// WireGuard tunnel. It cannot reuse gophermind-server/server.go's buildDeps
// directly: that function is unexported in a different "package main" (a
// different Go binary/module boundary), so it can't be imported here. This
// is instead a trimmed port of the same construction, using the exact same
// gophermind-lib packages buildDeps uses -- Pipeline, Skills, and Devices
// are omitted since these tests exercise session/chat/approval only.
//
// Serving the real mux in-process on the listener wireguard.Server.ListenTCP
// returns matches gophermind-server's own production architecture
// (startWireGuard passes that same listener straight to serve.Serve in one
// process): a separately spawned gophermind-server subprocess cannot bind
// inside another process's userspace WG netstack, which is why this exists
// instead of just exec.Command-ing the built binary the way e2e_local_test.go
// does for local mode.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"gophermind/gophermind-lib/agent"
	"gophermind/gophermind-lib/config"
	"gophermind/gophermind-lib/llm"
	"gophermind/gophermind-lib/project"
	"gophermind/gophermind-lib/prompt"
	"gophermind/gophermind-lib/safety"
	"gophermind/gophermind-lib/serve"
	"gophermind/gophermind-lib/session"
	"gophermind/gophermind-lib/tools"
)

// e2eLLMEndpoint resolves the same LLM endpoint gophermind-server's own
// resolveLLMEndpoint falls back to (the shared config's GOPHERMIND_BASE_URL)
// when GOPHERMIND_LLM_ENDPOINT is unset -- e2e_local_test.go's real spawned
// gophermind-server subprocess already depends on this same resolution
// working in this environment (it passes no --llm-endpoint flag either), so
// reusing it here keeps remote-mode E2E exercising the same real backend
// local-mode E2E already does, not a second, divergent one.
func e2eLLMEndpoint(t testing.TB) string {
	t.Helper()
	if v := os.Getenv("GOPHERMIND_LLM_ENDPOINT"); v != "" {
		return v
	}
	shared, err := config.Load()
	if err != nil {
		t.Fatalf("resolve LLM endpoint: config.Load: %v", err)
	}
	if shared.BaseURL == "" {
		t.Fatalf("resolve LLM endpoint: no GOPHERMIND_LLM_ENDPOINT and shared config has no BaseURL")
	}
	return shared.BaseURL
}

// e2eBuildRemoteMux builds a real serve.NewMux from real gophermind-lib
// components rooted at root, authenticated with token -- the same
// SessionTurn/Approvals/SessionMessages wiring gophermind-server/server.go's
// buildDeps uses for those fields, trimmed of Pipeline/Skills/Devices/
// ListModels (not exercised by the remote-mode tests this backs).
func e2eBuildRemoteMux(t testing.TB, root, token string) *http.ServeMux {
	t.Helper()

	llmClient := llm.New(e2eLLMEndpoint(t), "", "", 0, false)
	discoverCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	model, err := llmClient.DiscoverModel(discoverCtx)
	cancel()
	if err != nil {
		t.Fatalf("DiscoverModel: %v", err)
	}
	llmClient.Model = model

	reg := tools.NewRegistry(
		tools.ReadFileRange(root),
		tools.ListFilesGlob(root),
		tools.SearchEnhanced(root),
		tools.WriteFile(root),
		tools.EditFileMulti(root),
		tools.RunShellEnhanced(root, 120*time.Second, tools.ShellLimits{}),
		tools.FileStat(root),
		tools.MoveFile(root),
		tools.DeleteFile(root),
		tools.Mkdir(root),
		tools.PatchApply(root),
		tools.GitInfo(root),
	)

	pb, err := prompt.NewBuilder()
	if err != nil {
		t.Fatalf("prompt.NewBuilder: %v", err)
	}
	basePrompt := pb.Build()
	systemSuffix := project.Instructions(root)

	const llmMaxIter = 12
	approvals := serve.NewApprovalRegistry()
	approvalWait := serve.ServeApprovalTimeout()

	sessionTurn := func(ctx context.Context, id, task string, emit func(event, data string) error) error {
		onEvent := func(e agent.Event) {
			event, data, ok := serve.SSEFramesForAgentEvent(e)
			if !ok {
				return
			}
			_ = emit(event, data)
		}
		turnApprove := serve.RemoteApprovalGate(approvals, ctx, approvalWait, emit, serve.NewApprovalID)
		ag := agent.New(llmClient, reg, llmMaxIter, turnApprove, onEvent)
		if session.Exists(id) {
			if err := session.Load(id, ag); err != nil {
				return err
			}
		} else {
			ag.SetSystemPrompt(serve.SystemPromptForMode(serve.ReadSessionMode(id), basePrompt, root))
			if systemSuffix != "" {
				ag.AppendSystemPrompt(systemSuffix)
			}
		}
		if m := serve.ReadSessionModel(id); m != "" {
			ag.SetModel(m)
		}
		_, err := ag.Send(ctx, task)
		if serr := session.Save(id, ag); serr != nil && err == nil {
			err = serr
		}
		return err
	}

	// loadMessages backs GET /session/{id}/messages, which
	// TestE2E_RemoteMode_SessionManagement's "resume" step exercises --
	// same replay-via-ExportJSONL logic as gophermind-server/server.go's
	// buildDeps.loadMessages.
	loadMessages := func(id string) ([]json.RawMessage, bool, error) {
		if !session.Exists(id) {
			return nil, false, nil
		}
		ag := agent.New(llmClient, reg, llmMaxIter, safety.ApprovalFunc(safety.Auto), nil)
		if err := session.Load(id, ag); err != nil {
			return nil, true, err
		}
		var buf bytes.Buffer
		if err := ag.ExportJSONL(&buf); err != nil {
			return nil, true, err
		}
		var out []json.RawMessage
		sc := bufio.NewScanner(&buf)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 {
				continue
			}
			out = append(out, append(json.RawMessage(nil), line...))
		}
		return out, true, sc.Err()
	}

	d := serve.Deps{
		SessionTurn:     sessionTurn,
		Approvals:       approvals,
		SessionMessages: loadMessages,
	}

	mux, err := serve.NewMux(d, serve.Options{Token: token})
	if err != nil {
		t.Fatalf("serve.NewMux: %v", err)
	}
	return mux
}
