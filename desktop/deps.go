// Package main is the GopherMind desktop shell. deps.go builds the pieces
// internal/serve.Deps needs (LLM client, tool registry, system prompt) and
// assembles the narrow set of routes this task proves end to end: sessions
// and a chat turn.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"gophermind/internal/agent"
	"gophermind/internal/config"
	"gophermind/internal/llm"
	"gophermind/internal/safety"
	"gophermind/internal/serve"
	"gophermind/internal/session"
	"gophermind/internal/tools"
)

// loadConfig reads GopherMind's usual configuration (env vars, working-directory
// .env, and the global ~/.gophermind/config.json), exactly as `gophermind serve`
// does via config.Load, and validates it. The desktop app deliberately shares
// this configuration surface rather than inventing its own: the same
// GOPHERMIND_BASE_URL / GOPHERMIND_MODEL / etc. that configure the CLI also
// configure the embedded server.
func loadConfig() (config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

// newLLMClient builds and resolves an *llm.Client from cfg: it constructs the
// client with cfg's TLS options, applies the timeout/sampling settings, and
// resolves cfg.Model (auto-discovering it from the endpoint if unset). This
// mirrors the equivalent setup in cmd/gophermind's `serve` command, trimmed
// to what the desktop app's narrow Deps wiring needs.
func newLLMClient(ctx context.Context, cfg config.Config) (*llm.Client, error) {
	client, err := llm.NewWithTLS(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.LLMRequestTimeout(), llm.TLSOptions{
		InsecureSkipVerify: cfg.InsecureTLS,
		ClientCertPath:     cfg.ClientCertPath,
		ClientKeyPath:      cfg.ClientKeyPath,
		CACertPath:         cfg.CACertPath,
	})
	if err != nil {
		return nil, fmt.Errorf("TLS setup: %w", err)
	}
	client.ChatPath = cfg.ChatPath
	client.ModelsPath = cfg.ModelsPath
	client.SetStreamIdleTimeout(cfg.StreamIdleTimeout)
	client.Fallbacks = cfg.FallbackModels
	client.SetTemperature(cfg.Temperature)
	client.SetTopP(cfg.TopP)
	client.Retry = llm.RetryPolicy{
		MaxAttempts: cfg.MaxAttempts,
		BaseDelay:   cfg.RetryBaseDelay,
		MaxDelay:    llm.DefaultRetryPolicy.MaxDelay,
	}

	startupTimeout := cfg.HTTPTimeout
	if startupTimeout <= 0 {
		startupTimeout = 300 * time.Second
	}
	if cfg.Model == "" {
		discoverCtx, cancel := context.WithTimeout(ctx, startupTimeout)
		discovered, err := client.DiscoverModel(discoverCtx)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("no model set and discovery failed: %w (set GOPHERMIND_MODEL)", err)
		}
		client.Model = discovered
	} else {
		// Best-effort validation against /v1/models: a slow or unreachable
		// endpoint must not block startup, so any error here is ignored and
		// the configured model is used as-is.
		listCtx, cancel := context.WithTimeout(ctx, startupTimeout)
		models, err := client.ListModels(listCtx)
		cancel()
		if err == nil && len(models) > 0 && !slices.Contains(models, cfg.Model) {
			return nil, fmt.Errorf("model %q not found at %s", cfg.Model, cfg.BaseURL)
		}
	}
	return client, nil
}

// newToolRegistry builds the tool set the desktop chat agent runs with: file
// read/write/edit, search, shell, and basic filesystem operations rooted at
// cfg.RootDir. This is a deliberately smaller set than cmd/gophermind's full
// ~40-tool registry: most of the rest (web search, semantic index/memory,
// SQL, Jira/GitHub, MCP servers, plugins, ...) is wired through private
// helpers in package main of cmd/gophermind (secretEnv, docsTemplate,
// profileMemoryPath, ...) that are not exported for reuse here. Proving the
// embedded-server loop does not need them; a later task can grow this set.
func newToolRegistry(cfg config.Config) *tools.Registry {
	toolset := []tools.Tool{
		tools.ReadFileRange(cfg.RootDir),
		tools.ListFilesGlob(cfg.RootDir),
		tools.SearchEnhanced(cfg.RootDir),
		tools.WriteFile(cfg.RootDir),
		tools.EditFileMulti(cfg.RootDir),
		tools.RunShellEnhanced(cfg.RootDir, cfg.CmdTimeout, tools.ShellLimits{
			CPUSeconds:  cfg.ShellCPUSeconds,
			MaxMemoryMB: cfg.ShellMaxMemMB,
			MaxProcs:    cfg.ShellMaxProcs,
		}),
		tools.FileStat(cfg.RootDir),
		tools.MoveFile(cfg.RootDir),
		tools.DeleteFile(cfg.RootDir),
		tools.Mkdir(cfg.RootDir),
		tools.PatchApply(cfg.RootDir),
	}
	return tools.NewRegistry(toolset...)
}

// newServeDeps assembles the narrow serve.Deps this task wires: Run, Stream,
// SessionTurn, SessionMessages, and ListModels. Approvals, Devices, and
// Metrics are intentionally left nil (see the desktop-app task report for
// why), NewMux skips the routes that depend on a nil field, so /session/*
// still starts up correctly, just without the approve/devices/metrics
// endpoints.
//
// Every gated (mutating) tool call is auto-approved (safety.Auto). Remote
// approval, the APNs push path, and the judge/policy gates that
// cmd/gophermind's `serve` command layers on are out of scope for this task:
// the Approvals screen (a later task) is what makes gating meaningful in a
// GUI, and Deps.Approvals is nil until it exists.
func newServeDeps(client *llm.Client, reg *tools.Registry, cfg config.Config, basePrompt string) serve.Deps {
	approve := safety.Auto

	run := func(ctx context.Context, t string) (string, error) {
		ag := agent.New(client, reg, cfg.MaxIter, approve, nil)
		ag.SetPrices(cfg.InputPricePer1K, cfg.OutputPricePer1K)
		ag.SetSystemPrompt(basePrompt)
		answer, err := ag.Send(ctx, t)
		return answer, err
	}

	stream := func(ctx context.Context, t string, emit func(string)) error {
		ag := agent.New(client, reg, cfg.MaxIter, approve, func(e agent.Event) {
			if e.Type == "token" {
				emit(e.Text)
			}
		})
		ag.SetPrices(cfg.InputPricePer1K, cfg.OutputPricePer1K)
		ag.SetSystemPrompt(basePrompt)
		_, err := ag.Send(ctx, t)
		return err
	}

	sessionTurn := func(ctx context.Context, id, t string, emit func(event, data string) error) error {
		onEvent := func(e agent.Event) {
			event, data, ok := serve.SSEFramesForAgentEvent(e)
			if !ok {
				return
			}
			_ = emit(event, data)
		}
		ag := agent.New(client, reg, cfg.MaxIter, approve, onEvent)
		ag.SetPrices(cfg.InputPricePer1K, cfg.OutputPricePer1K)
		if session.Exists(id) {
			if err := session.Load(id, ag); err != nil {
				return err
			}
		} else {
			ag.SetSystemPrompt(serve.SystemPromptForMode(serve.ReadSessionMode(id), basePrompt, cfg.RootDir))
		}
		if m := serve.ReadSessionModel(id); m != "" {
			ag.SetModel(m)
		}
		_, err := ag.Send(ctx, t)
		if serr := session.Save(id, ag); serr != nil && err == nil {
			err = serr
		}
		return err
	}

	loadMessages := func(id string) ([]json.RawMessage, bool, error) {
		if !session.Exists(id) {
			return nil, false, nil
		}
		ag := agent.New(client, reg, cfg.MaxIter, approve, nil)
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

	listModels := func() ([]string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return client.ListModels(ctx)
	}

	return serve.Deps{
		Run:             run,
		Stream:          stream,
		SessionTurn:     sessionTurn,
		SessionMessages: loadMessages,
		ListModels:      listModels,
		// Metrics, Approvals, Devices: left nil. See the doc comment above.
	}
}
