package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
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

// llmMaxIter bounds a single served turn's tool-call loop. Fixed rather than
// configurable for now: gophermind-server has no --max-iter flag, matching
// its narrow 02-01/02-02 scope. cfg.MaxIter in gophermind-lib/config.Config
// exists for the CLI's own use and is not read here, deliberately -- this is
// a distinct concern (see serverConfig's own doc comment).
const llmMaxIter = 12

// buildDeps constructs a real, working serve.Deps: an LLM client against
// cfg.LLMEndpoint, a core file/shell/search tool registry rooted at root, and
// Run/Stream/SessionTurn closures built the same way gophermind's own `serve`
// command builds them (cmd/gophermind/main.go's "case \"serve\":" block) --
// one fresh agent.Agent per turn, sharing one approvalRegistry so
// POST /session/{id}/approve can resolve any turn's pending gate.
//
// Session turns always go through serve.RemoteApprovalGate: this is a
// headless server with no terminal to answer an interactive prompt, so every
// gated tool call blocks on an "approval-needed" SSE frame until the client
// resolves it (or it times out). Run/Stream (POST /run, /run/stream) have no
// session/client to notify, so they use safety.Auto -- matching the CLI
// webhook's own choice for the same reason (see main.go's "serve" case).
// buildDeps' second return value is the pipeline hub, exposed separately
// (rather than only inside serve.Deps.Pipeline.Hub) because the caller must
// also start serve.StartPipelineWatcher against it -- Deps alone doesn't
// carry enough to do that without a type assertion.
func buildDeps(cfg serverConfig, root string, logger *slog.Logger) (serve.Deps, *serve.PipelineHub, error) {
	client := llm.New(cfg.LLMEndpoint, "", "", 0, false)

	// A Client with no Model set makes every Complete/Stream call fail with
	// llm.ErrNoModel (see gophermind-lib/llm/fallback.go) rather than the
	// nil-pointer panic that used to happen before that fix existed. Still,
	// this call is what actually gets a real model wired in -- matching
	// cmd/gophermind/main.go's own startup sequence for the CLI's "serve"
	// command. A failure here is NOT fatal to buildDeps: an unreachable or
	// unconfigured LLM endpoint shouldn't stop the server from serving
	// health/ready/metrics/session-CRUD (everything except actually running
	// a turn); ErrNoModel then correctly rejects Run/Stream/SessionTurn
	// calls with a clear error instead of this function refusing to start
	// the whole server over it.
	if cfg.LLMEndpoint != "" {
		discoverCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		model, err := client.DiscoverModel(discoverCtx)
		cancel()
		if err != nil {
			logger.Warn("model discovery failed; Run/Stream/SessionTurn will error until this is fixed", "error", err)
		} else {
			client.Model = model
		}
	}

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
		return serve.Deps{}, nil, err
	}
	basePrompt := pb.Build()
	systemSuffix := project.Instructions(root)

	metrics := &serve.ServeMetrics{}
	approvals := serve.NewApprovalRegistry()
	approvalWait := serve.ServeApprovalTimeout()

	// listModels backs both /models (auth-gated model list) and
	// /models/catalogue's local-endpoint entries: a bounded-timeout proxy of
	// the configured endpoint's model list, so a slow/unreachable LLM
	// endpoint can't hang a request indefinitely.
	listModels := func() ([]string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return client.ListModels(ctx)
	}

	run := func(ctx context.Context, task string) (string, error) {
		ag := agent.New(client, reg, llmMaxIter, safety.ApprovalFunc(safety.Auto), nil)
		ag.SetSystemPrompt(basePrompt)
		if systemSuffix != "" {
			ag.AppendSystemPrompt(systemSuffix)
		}
		answer, err := ag.Send(ctx, task)
		u := ag.Usage()
		metrics.PromptTokens.Add(int64(u.PromptTokens))
		metrics.CompletionTokens.Add(int64(u.CompletionTokens))
		return answer, err
	}

	stream := func(ctx context.Context, task string, emit func(string)) error {
		ag := agent.New(client, reg, llmMaxIter, safety.ApprovalFunc(safety.Auto), func(e agent.Event) {
			if e.Type == "token" {
				emit(e.Text)
			}
		})
		ag.SetSystemPrompt(basePrompt)
		if systemSuffix != "" {
			ag.AppendSystemPrompt(systemSuffix)
		}
		_, err := ag.Send(ctx, task)
		return err
	}

	sessionTurn := func(ctx context.Context, id, task string, emit func(event, data string) error) error {
		onEvent := func(e agent.Event) {
			event, data, ok := serve.SSEFramesForAgentEvent(e)
			if !ok {
				return
			}
			_ = emit(event, data)
		}
		turnApprove := serve.RemoteApprovalGate(approvals, ctx, approvalWait, emit, serve.NewApprovalID)
		ag := agent.New(client, reg, llmMaxIter, turnApprove, onEvent)
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
		u := ag.Usage()
		metrics.PromptTokens.Add(int64(u.PromptTokens))
		metrics.CompletionTokens.Add(int64(u.CompletionTokens))
		if serr := session.Save(id, ag); serr != nil && err == nil {
			err = serr
		}
		return err
	}

	// loadMessages backs GET /session/{id}/messages: it replays a saved
	// session into a fresh agent and re-exports its history as JSONL, so a
	// client can render the transcript of a session it did not just create
	// in this process. Mirrors cmd/gophermind/main.go's identical helper for
	// its own "serve" command.
	loadMessages := func(id string) ([]json.RawMessage, bool, error) {
		if !session.Exists(id) {
			return nil, false, nil
		}
		ag := agent.New(client, reg, llmMaxIter, safety.ApprovalFunc(safety.Auto), nil)
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

	pipelineHub := serve.NewPipelineHub()
	skillCfgDir, _ := config.Dir()

	// EndpointModels wants func() []string (no error, per catalogue's "best
	// effort" contract: a failed probe just means the catalogue omits
	// local-endpoint entries, not that the whole request fails), while
	// ListModels wants func() ([]string, error) (GET /models can report a
	// real failure). Same underlying call, two shapes.
	endpointModels := func() []string {
		names, err := listModels()
		if err != nil {
			return nil
		}
		return names
	}

	d := serve.Deps{
		Run: run, Stream: stream, Metrics: metrics,
		SessionTurn:     sessionTurn,
		Approvals:       approvals,
		SessionMessages: loadMessages,
		ListModels:      listModels,
		EndpointModels:  endpointModels,
		Pipeline:        &serve.PipelineDeps{Root: root, Hub: pipelineHub},
		Skills:          &serve.SkillsDeps{Root: root, ConfigDir: skillCfgDir},
	}

	logger.Info("deps built", "llm_endpoint_configured", cfg.LLMEndpoint != "", "root", root)
	return d, pipelineHub, nil
}
