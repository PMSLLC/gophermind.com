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
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"gophermind/internal/agent"
	"gophermind/internal/config"
	"gophermind/internal/freellm"
	"gophermind/internal/llm"
	"gophermind/internal/modelcat"
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

// livenessProbeTimeout bounds the best-effort startup checks in
// newLLMClient (model discovery when cfg.Model is unset, and the /v1/models
// validation when it is set). These are liveness checks of the endpoint, not
// real completion requests, so they must not share cfg's much longer request
// timeout: an unreachable endpoint needs to fail fast so startup can fall
// back to a free provider instead of hanging.
const livenessProbeTimeout = 5 * time.Second

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

	if cfg.Model == "" {
		discoverCtx, cancel := context.WithTimeout(ctx, livenessProbeTimeout)
		discovered, err := client.DiscoverModel(discoverCtx)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("no model set and discovery failed: %w (set GOPHERMIND_MODEL)", err)
		}
		client.Model = discovered
	} else {
		// Liveness probe against /v1/models, bounded tightly by
		// livenessProbeTimeout rather than cfg's request timeout: this is a
		// short "is anything there" check, not a real request. Unlike the
		// stale comment this replaces, an error here is NOT ignored: it is
		// returned to the caller, which treats it as the endpoint being
		// unusable and falls back (see resolveLLMBackend in server.go).
		listCtx, cancel := context.WithTimeout(ctx, livenessProbeTimeout)
		models, err := client.ListModels(listCtx)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("endpoint %s unreachable: %w", cfg.BaseURL, err)
		}
		if len(models) > 0 && !slices.Contains(models, cfg.Model) {
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
func newToolRegistry(cfg config.Config, getClient func() (*llm.Client, error)) *tools.Registry {
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
		// humanize resolves the client at call time: the registry is built
		// before the LLM backend is known, and a nil getClient simply yields
		// a tool that reports it is unconfigured.
		tools.Humanize(func(ctx context.Context, system, user string) (string, error) {
			if getClient == nil {
				return "", errors.New("no model configured")
			}
			c, err := getClient()
			if err != nil {
				return "", err
			}
			msg, _, err := c.Complete(ctx, []llm.Message{
				{Role: "system", Content: system},
				{Role: "user", Content: user},
			}, nil)
			if err != nil {
				return "", err
			}
			return msg.Content, nil
		}),
		tools.MoveFile(cfg.RootDir),
		tools.DeleteFile(cfg.RootDir),
		tools.Mkdir(cfg.RootDir),
		tools.PatchApply(cfg.RootDir),
	}
	return tools.NewRegistry(toolset...)
}

// desktopApprovalTimeout bounds how long a pending gated tool call waits for
// a human decision in the desktop app before auto-denying. It is
// deliberately longer than internal/serve's own default
// (serve.ServeApprovalTimeout, 5 minutes): that default is tuned for a push
// notification to a phone the user may not be looking at, whereas the
// desktop app's whole point is a person looking directly at the window, who
// may still want time to read a shell command or a diff before deciding.
const desktopApprovalTimeout = 30 * time.Minute

// oneShotGate is the approval policy for the one-shot /run and /run/stream
// routes: it refuses every gated (mutating) tool call and records what it
// refused. Those routes have no interactive channel a pending approval could
// be raised on, so "ask a human" is not available and auto-approving would
// make the approvals gate optional; refusing is the only answer that keeps
// the guarantee. The recorded tool names let the caller be told a refusal
// happened, since the agent otherwise reports a denial only into the model's
// own transcript.
type oneShotGate struct {
	mu      sync.Mutex
	refused []string
}

// approve implements safety.ApprovalFunc. It denies gated tools and allows
// everything else, so read-only tools still work on these routes.
func (g *oneShotGate) approve(tool, argsJSON string) bool {
	if !safety.Gated(tool) {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !slices.Contains(g.refused, tool) {
		g.refused = append(g.refused, tool)
	}
	return false
}

// notice returns the line reporting the refusal to the caller, or "" when
// nothing was refused.
func (g *oneShotGate) notice() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.refused) == 0 {
		return ""
	}
	return "[gophermind] refused: this route cannot ask anyone for approval, so these gated tool calls were denied: " +
		strings.Join(g.refused, ", ")
}

// newServeDeps assembles the narrow serve.Deps this task wires: Run, Stream,
// SessionTurn, SessionMessages, ListModels, and Approvals. Metrics and
// Devices are still left nil: there is no metrics scrape target and no APNs
// push destination for a desktop window, so NewMux simply skips those two
// routes.
//
// getClient looks up the *llm.Client currently in use, rather than a client
// being passed directly, so serve.Deps can be built (and the embedded server
// started) before the LLM backend has finished resolving: see
// resolveLLMBackend in server.go. It returns a clear error when no backend
// is available yet (or resolution failed outright), which every closure
// below surfaces to its caller instead of touching a nil client.
//
// Every gated (mutating) tool call made through a session turn now blocks on
// a real human decision instead of auto-approving: sessionTurn wraps the
// shared approvals registry in serve.RemoteApprovalGate, the same machinery
// cmd/gophermind's remote (phone) approval path uses, and hands it the
// session turn's own SSE emit function so the "approval-needed" frame
// reaches the frontend on the same open stream the rest of the turn's
// output uses. This is the Approvals screen the desktop design doc calls
// the reason the app is worth building.
//
// Run and Stream (the one-shot /run and /run/stream routes) refuse gated
// tool calls outright, via oneShotGate: neither carries a session id or an
// SSE emit function a pending approval could be raised on and resolved
// against, so there is no human to ask. They used to pass safety.Auto, which
// made the app's "every mutating tool blocks on a human" guarantee
// bypassable by hitting a different route on the same mux with the same
// token. That the desktop frontend does not call these routes was never a
// control, only a coincidence.
//
// Before each session turn, unless the session has pinned an explicit model
// (serve.ReadSessionModel), the model picker's policy (modelcat.Next) is
// evaluated once: see applyModelPolicy. getProfile reports which gophermind
// profile the client from getClient currently belongs to, and setBackend
// installs a new active client (and its profile) when the policy switches
// to a different provider; both are satisfied by a *clientHolder's Profile
// and Set methods in production.
func newServeDeps(getClient func() (*llm.Client, error), getProfile func() string, setBackend func(*llm.Client, string), reg *tools.Registry, cfg config.Config, basePrompt string) serve.Deps {
	approvals := serve.NewApprovalRegistry()

	run := func(ctx context.Context, t string) (string, error) {
		client, err := getClient()
		if err != nil {
			return "", err
		}
		gate := &oneShotGate{}
		ag := agent.New(client, reg, cfg.MaxIter, gate.approve, nil)
		ag.SetPrices(cfg.InputPricePer1K, cfg.OutputPricePer1K)
		ag.SetSystemPrompt(basePrompt)
		answer, err := ag.Send(ctx, t)
		if notice := gate.notice(); notice != "" {
			answer += "\n\n" + notice
		}
		return answer, err
	}

	stream := func(ctx context.Context, t string, emit func(string)) error {
		client, err := getClient()
		if err != nil {
			return err
		}
		gate := &oneShotGate{}
		ag := agent.New(client, reg, cfg.MaxIter, gate.approve, func(e agent.Event) {
			if e.Type == "token" {
				emit(e.Text)
			}
		})
		ag.SetPrices(cfg.InputPricePer1K, cfg.OutputPricePer1K)
		ag.SetSystemPrompt(basePrompt)
		_, err = ag.Send(ctx, t)
		if notice := gate.notice(); notice != "" {
			emit("\n\n" + notice)
		}
		return err
	}

	sessionTurn := func(ctx context.Context, id, t string, emit func(event, data string) error) error {
		client, err := getClient()
		if err != nil {
			return err
		}

		// Evaluate the model picker's policy once, before the turn starts
		// (see "Switching happens between turns" on applyModelPolicy). A
		// session that has pinned an explicit model is left alone: that
		// choice is the user's, and automatic cycling must never override
		// it silently.
		//
		// A pinned model runs on a clone of the active client rather than on
		// the client itself. llm.Client.Model is a plain field with no mutex
		// and the client is process-wide, so setting it per turn both raced
		// with every concurrent turn and let the last writer decide which
		// model a sibling's request went out on.
		var switchChoice modelcat.Choice
		switched := false
		if pinnedProfile, pinnedModel := serve.ReadSessionBackend(id); pinnedModel != "" {
			// A pinned model on ANOTHER provider needs that provider's
			// endpoint and key, not just its name: CloneForModel keeps the
			// current BaseURL, so pinning a Kilo Code model while the client
			// is on OVHcloud would POST that id to OVHcloud and 404. When the
			// pin names no profile it belongs to the active endpoint, which
			// is what the old bare-model sidecar meant.
			if pinnedProfile != "" && pinnedProfile != getProfile() {
				if c, err := clientForProfile(ctx, cfg, pinnedProfile, pinnedModel); err == nil {
					setBackend(c, pinnedProfile)
					client = c
				} else {
					// Fall back to the active endpoint rather than failing the
					// turn, and say so: a pinned provider that cannot be
					// reached is worth reporting, not worth losing the turn
					// over.
					_ = emit("model-error", `{"profile":`+strconv.Quote(pinnedProfile)+
						`,"error":`+strconv.Quote(err.Error())+`}`)
					client = client.CloneForModel(pinnedModel)
				}
			} else {
				client = client.CloneForModel(pinnedModel)
			}
		} else {
			client, switchChoice, switched = applyModelPolicy(ctx, cfg, getProfile(), client, setBackend)
		}

		onEvent := func(e agent.Event) {
			event, data, ok := serve.SSEFramesForAgentEvent(e)
			if !ok {
				return
			}
			_ = emit(event, data)
		}
		// The gate is built per turn (not once for the registry's lifetime)
		// because it closes over this turn's ctx and emit: a pending
		// approval must deny on this turn's client disconnect, not some
		// other turn's, and the "approval-needed" frame must land on this
		// turn's own SSE stream.
		turnApprove := serve.RemoteApprovalGate(approvals, ctx, desktopApprovalTimeout, emit, serve.NewApprovalID)
		// Give this turn a fallback list so a 429 moves to another model
		// instead of failing. OVHcloud's anonymous tier caps at 2 requests
		// per minute PER MODEL, so a sibling model on the same provider has
		// its own budget and is a real escape, not a retry in disguise.
		//
		// Same provider only: llm.Client swaps the model string and keeps the
		// endpoint, so another provider's id would 404 here.
		client = withFallbacks(client, cfg, getProfile())

		// The tool registry is per turn when the session has its own working
		// directory, because every tool captures its root at construction:
		// ReadFileRange, WriteFile and RunShellEnhanced all close over
		// cfg.RootDir, and safety.SafeJoin contains paths against whatever
		// root they were given. A session pointed at another project
		// therefore needs its own registry, not a flag passed at call time.
		//
		// The shared one is reused when there is no override, so the common
		// case costs nothing and the behaviour is byte-identical to before.
		turnReg := reg
		turnRoot := cfg.RootDir
		if r := serve.ReadSessionRoot(id); r != "" && r != cfg.RootDir {
			turnCfg := cfg
			turnCfg.RootDir = r
			turnReg = newToolRegistry(turnCfg, getClient)
			turnRoot = r
		}
		ag := agent.New(client, turnReg, cfg.MaxIter, turnApprove, onEvent)
		ag.SetPrices(cfg.InputPricePer1K, cfg.OutputPricePer1K)
		if switched {
			b, _ := json.Marshal(struct {
				Profile string `json:"profile"`
				Model   string `json:"model"`
				Reason  string `json:"reason"`
			}{switchChoice.Profile, switchChoice.Model, switchChoice.Reason})
			_ = emit("model-switched", string(b))
		}
		if session.Exists(id) {
			if err := session.Load(id, ag); err != nil {
				return err
			}
		} else {
			// turnRoot, not cfg.RootDir: the prompt has to name the directory
			// this session's tools actually operate in, or the agent is told
			// it is in one project while reading and writing another.
			ag.SetSystemPrompt(serve.SystemPromptForMode(serve.ReadSessionMode(id), basePrompt, turnRoot))
		}
		_, err = ag.Send(ctx, t)
		// Meter the turn against the profile that served it. Nothing in this
		// app recorded usage before: the only production writer was the CLI's
		// one-shot run/ask path, so the model picker read an odometer that
		// was always zero. Every model showed its full allowance forever and
		// cycle-on-capacity could never fire, because nothing ever
		// approached capacity. getProfile reports the profile AFTER any
		// switch applyModelPolicy just made, and ag.LLM().Model is the model
		// that actually served the turn.
		u := ag.Usage()
		_ = freellm.Record(getProfile(), ag.LLM().Model, u.PromptTokens, u.CompletionTokens)
		if serr := session.Save(id, ag); serr != nil && err == nil {
			err = serr
		}
		return err
	}

	loadMessages := func(id string) ([]json.RawMessage, bool, error) {
		if !session.Exists(id) {
			return nil, false, nil
		}
		client, err := getClient()
		if err != nil {
			return nil, true, err
		}
		ag := agent.New(client, reg, cfg.MaxIter, (&oneShotGate{}).approve, nil)
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
		client, err := getClient()
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return client.ListModels(ctx)
	}

	return serve.Deps{
		Run:             run,
		Stream:          stream,
		SessionTurn:     sessionTurn,
		Skills:          skillsDeps(cfg),
		SessionMessages: loadMessages,
		ListModels:      listModels,
		Approvals:       approvals,
		// Metrics, Devices: still left nil. See the doc comment above.
	}
}

// applyModelPolicy evaluates modelcat.Next before a turn starts and, when it
// chooses a different model, switches to it, returning the client the turn
// should actually use. Switching happens here and nowhere else: a turn's
// context is built for one model's token budget and tool dialect, so this
// runs once, before agent.New, never mid-turn.
//
// The catalogue it builds carries no live listing of the active endpoint's
// own models (Build's endpointModels is nil), so this makes no network
// call of its own: the odometer and settings are local files, and an
// endpoint-served model never carries a published quota anyway (there is
// no line for it to cross), so it would never be a switch candidate under
// rule 4 regardless.
//
// When the chosen entry is on the same profile as the current client, no new
// connection is needed: the switch is a clone of the current client carrying
// the chosen model, installed through setBackend as the new active client.
// It is a clone rather than an assignment to client.Model because that field
// carries no mutex and the client is shared by every concurrent turn, so
// writing it raced with them. When the choice names a different profile, a new
// client is built for that profile, with its own BaseURL, API key and
// paths, via clientForProfile, mirroring how resolveLLMBackend builds the
// startup fallback client; on success it is installed as the app's active
// backend through setBackend, replacing the client every session's turns
// use from now on. If the chosen profile cannot actually be reached (the
// catalogue's Reachable flag can go stale between builds), the current
// client and model are kept: a failed proactive switch must never fail the
// turn it exists to protect.
func applyModelPolicy(ctx context.Context, cfg config.Config, currentProfile string, client *llm.Client, setBackend func(*llm.Client, string)) (*llm.Client, modelcat.Choice, bool) {
	o, _ := freellm.LoadOdometer(freellm.OdometerPath())
	// A settings read failure must not fail a turn, so the current client is
	// kept and no automatic switch is considered at all. Choosing a model
	// from settings that are not the user's could auto-select a provider
	// whose terms they excluded, which is the one outcome this must never
	// produce; not switching can only ever leave them where they already are.
	s, err := modelcat.LoadSettings(modelcat.SettingsPath())
	if err != nil {
		return client, modelcat.Choice{}, false
	}
	entries := modelcat.Build(o, s, nil, time.Now())

	choice := modelcat.Next(entries, s, currentProfile, client.Model)
	if !choice.Switched {
		return client, choice, false
	}
	if choice.Profile == currentProfile {
		next := client.CloneForModel(choice.Model)
		setBackend(next, currentProfile)
		return next, choice, true
	}
	newClient, err := clientForProfile(ctx, cfg, choice.Profile, choice.Model)
	if err != nil {
		return client, modelcat.Choice{}, false
	}
	setBackend(newClient, choice.Profile)
	return newClient, choice, true
}

// clientForProfile builds a client for switching the active backend to a
// different profile mid-run. It resolves BaseURL, API key, ChatPath and
// ModelsPath for profile via config.ApplyProfile, the same mechanism
// resolveLLMBackend uses to build the startup fallback client, then forces
// Model to modelID rather than the profile's own default model, since
// modelID is the exact model modelcat.Next chose.
func clientForProfile(ctx context.Context, base config.Config, profile, modelID string) (*llm.Client, error) {
	pcfg := base
	pcfg.Profile = profile
	applied, err := pcfg.ApplyProfile()
	if err != nil {
		return nil, fmt.Errorf("apply profile %q: %w", profile, err)
	}
	applied.Model = modelID
	return newLLMClient(ctx, applied)
}

// skillsDeps points the skill routes at this project and the user's config
// directory. A config directory that cannot be resolved disables the routes
// rather than guessing a path: writing skill settings somewhere unexpected is
// worse than not offering the panel.
func skillsDeps(cfg config.Config) *serve.SkillsDeps {
	dir, err := config.Dir()
	if err != nil {
		return nil
	}
	return &serve.SkillsDeps{Root: cfg.RootDir, ConfigDir: dir}
}

// withFallbacks returns a client that will try sibling models on the same
// provider when a request fails with something fallback-eligible, chiefly a
// 429.
//
// It clones rather than mutating: the holder's client is shared across
// concurrent turns, and assigning Fallbacks on it would be the same
// shared-mutable-state bug that CloneForModel exists to avoid.
//
// A catalogue that cannot be read is not an error. The turn simply runs
// without fallbacks, exactly as it did before this existed.
func withFallbacks(c *llm.Client, cfg config.Config, profile string) *llm.Client {
	if c == nil || profile == "" {
		return c
	}
	o, err := freellm.LoadOdometer(freellm.OdometerPath())
	if err != nil {
		return c
	}
	s, err := modelcat.LoadSettings(modelcat.SettingsPath())
	if err != nil {
		return c
	}
	fb := modelcat.FallbackModels(modelcat.Build(o, s, nil, time.Now()), s, profile, c.Model)
	if len(fb) == 0 {
		return c
	}
	out := c.CloneForModel(c.Model)
	out.Fallbacks = fb
	return out
}
