// Package setup implements gophermind's first-run configuration wizard: an
// interactive prompt sequence that captures the essentials (endpoint, API key,
// model, approval mode) and reports them as GOPHERMIND_* pairs for the caller to
// persist with config.Save. It has no dependency on the llm client or
// TUI: model discovery and secret reading are injected, so the flow is fully
// testable without a terminal or a live endpoint.
package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// defaultMaxIter is the loop-iteration budget used when the wizard is given no
// value and Defaults carries none. It mirrors config's GOPHERMIND_MAX_ITER default.
const defaultMaxIter = 25

// Result is the set of values the wizard captured.
type Result struct {
	BaseURL      string
	APIKey       string
	Model        string
	ApprovalMode string // "ask" | "auto"
	MaxIter      int    // agent loop-iteration budget per turn

	// ChatPath is config.Config.ChatPath, carried through from whichever
	// profile menu entry was chosen (empty for a custom URL or a profile
	// whose base URL needs no override). Empty means the client's default
	// "/v1/chat/completions", so a wizard run that never touches this field
	// behaves exactly as before it existed.
	ChatPath string
	// ModelsPath is config.Config.ModelsPath, carried through the same way as
	// ChatPath and for the same reason: empty means the client's default
	// "/v1/models".
	ModelsPath string

	// Optional integration credentials.
	BraveAPIKey   string
	GitHubToken   string
	NotifyWebhook string

	// SpeedModel is GOPHERMIND_SPEED_MODEL: the faster/cheaper tier used for
	// /project-execute's "speed" task assignments (empty falls back to
	// Model, the same as an unset env var).
	SpeedModel string
}

// Pairs renders the result as ordered GOPHERMIND_* env pairs for persistence.
// BaseURL, ChatPath, and ModelsPath are always emitted, even empty:
// config.Save treats an empty value as "delete this key", and a wizard run
// always resolves a definite ChatPath/ModelsPath (a chosen profile's value,
// or empty for one that needs no override), an empty result IS the
// intended state, not a blank answer to ignore. Omitting them when empty
// would leave a stale chat_path/models_path from an earlier run in place
// after switching to a profile that does not need one (e.g. openai ->
// local-llama), which would silently reintroduce the doubled-path bug this
// field exists to fix. Every OTHER optional value (API key, model) is
// omitted when blank so a blank answer never writes a spurious line or
// clobbers a hand-set value. Those, unlike the paths, are genuinely
// "leave unset" rather than "explicitly empty".
func (r Result) Pairs() [][2]string {
	pairs := [][2]string{
		{"GOPHERMIND_BASE_URL", r.BaseURL},
		{"GOPHERMIND_CHAT_PATH", r.ChatPath},
		{"GOPHERMIND_MODELS_PATH", r.ModelsPath},
	}
	if r.APIKey != "" {
		pairs = append(pairs, [2]string{"GOPHERMIND_API_KEY", r.APIKey})
	}
	if r.Model != "" {
		pairs = append(pairs, [2]string{"GOPHERMIND_MODEL", r.Model})
	}
	pairs = append(pairs, [2]string{"GOPHERMIND_APPROVAL", r.ApprovalMode})
	if r.MaxIter > 0 {
		pairs = append(pairs, [2]string{"GOPHERMIND_MAX_ITER", strconv.Itoa(r.MaxIter)})
	}
	if r.BraveAPIKey != "" {
		pairs = append(pairs, [2]string{"GOPHERMIND_BRAVE_API_KEY", r.BraveAPIKey})
	}
	if r.GitHubToken != "" {
		pairs = append(pairs, [2]string{"GITHUB_TOKEN", r.GitHubToken})
	}
	if r.NotifyWebhook != "" {
		pairs = append(pairs, [2]string{"GOPHERMIND_NOTIFY_WEBHOOK", r.NotifyWebhook})
	}
	if r.SpeedModel != "" {
		pairs = append(pairs, [2]string{"GOPHERMIND_SPEED_MODEL", r.SpeedModel})
	}
	return pairs
}

// Options configures a wizard run. In/Out are the I/O streams; Profiles is the
// endpoint menu ({name, baseURL, chatPath, modelsPath}); ListModels fetches
// selectable models for the chosen endpoint, given the resolved modelsPath
// for that endpoint (from the chosen profile's quad, or Defaults.ModelsPath
// for a custom URL) so it can probe the same path the endpoint actually
// serves models from instead of always assuming the client's bare default;
// ReadSecret reads the API key without echo (nil => read a plain line from
// In); Defaults pre-fills answers when re-running.
type Options struct {
	In         io.Reader
	Out        io.Writer
	Profiles   [][4]string
	ListModels func(baseURL, modelsPath, apiKey string) ([]string, error)
	ReadSecret func() (string, error)
	Defaults   Result
}

// Run executes the interactive wizard and returns the captured Result.
func Run(opts Options) (Result, error) {
	r := bufio.NewReader(opts.In)
	out := opts.Out
	readLine := func() (string, error) {
		s, err := r.ReadString('\n')
		// EOF is not a failure: a final line without a newline still carries
		// content, and once input is exhausted later prompts fall back to their
		// defaults rather than aborting the wizard. Other read errors propagate.
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimRight(s, "\r\n"), nil
	}

	// 1) Endpoint: built-in menu or a custom URL.
	fmt.Fprintln(out, "Endpoint:")
	for i, p := range opts.Profiles {
		fmt.Fprintf(out, "  %d) %s (%s)\n", i+1, p[0], p[1])
	}
	customIdx := len(opts.Profiles) + 1
	fmt.Fprintf(out, "  %d) custom URL\n", customIdx)
	fmt.Fprint(out, "Choose [1]: ")
	choiceLine, err := readLine()
	if err != nil {
		return Result{}, err
	}
	choice := parseIntOr(choiceLine, 1)

	var baseURL, chatPath, modelsPath string
	if choice >= 1 && choice <= len(opts.Profiles) {
		baseURL = opts.Profiles[choice-1][1]
		chatPath = opts.Profiles[choice-1][2]
		modelsPath = opts.Profiles[choice-1][3]
	} else {
		// Custom (or out-of-range): prompt for a URL, defaulting to any prior value.
		def := opts.Defaults.BaseURL
		fmt.Fprintf(out, "Base URL%s: ", defaultHint(def))
		line, err := readLine()
		if err != nil {
			return Result{}, err
		}
		baseURL = firstNonEmpty(strings.TrimSpace(line), def)
		// A custom URL carries no known chat/models path; fall back to
		// whatever the prior run had (e.g. re-running the wizard against the
		// same custom endpoint), which is empty (the client defaults) the
		// first time.
		chatPath = opts.Defaults.ChatPath
		modelsPath = opts.Defaults.ModelsPath
	}

	// 2) API key (optional, read without echo when a ReadSecret is provided).
	fmt.Fprint(out, "API key (blank = none): ")
	var apiKey string
	if opts.ReadSecret != nil {
		apiKey, err = opts.ReadSecret()
		if err != nil {
			return Result{}, err
		}
	} else {
		line, err := readLine()
		if err != nil {
			return Result{}, err
		}
		apiKey = strings.TrimSpace(line)
	}

	// 3) Model: pick from live discovery, else free-text.
	var model string
	models, listErr := listModels(opts, baseURL, modelsPath, apiKey)
	if listErr == nil && len(models) > 0 {
		fmt.Fprintln(out, "Model:")
		for i, m := range models {
			fmt.Fprintf(out, "  %d) %s\n", i+1, m)
		}
		fmt.Fprint(out, "Choose [1]: ")
		line, err := readLine()
		if err != nil {
			return Result{}, err
		}
		n := parseIntOr(line, 1)
		if n < 1 || n > len(models) {
			n = 1
		}
		model = models[n-1]
	} else {
		if listErr != nil {
			fmt.Fprintf(out, "(could not list models: %v)\n", listErr)
		}
		fmt.Fprintf(out, "Model (blank = auto-discover)%s: ", defaultHint(opts.Defaults.Model))
		line, err := readLine()
		if err != nil {
			return Result{}, err
		}
		model = firstNonEmpty(strings.TrimSpace(line), opts.Defaults.Model)
	}

	// 4) Approval mode.
	fmt.Fprint(out, "Approval mode ask/auto [ask]: ")
	line, err := readLine()
	if err != nil {
		return Result{}, err
	}
	mode := "ask"
	if strings.EqualFold(strings.TrimSpace(line), "auto") {
		mode = "auto"
	}

	// 5) Max iterations per turn: how many tool-loop passes the agent may take
	// before returning without a final answer.
	maxDefault := opts.Defaults.MaxIter
	if maxDefault < 1 {
		maxDefault = defaultMaxIter
	}
	fmt.Fprintf(out, "Max iterations per turn [%d]: ", maxDefault)
	line, err = readLine()
	if err != nil {
		return Result{}, err
	}
	maxIter := parseIntOr(line, maxDefault)
	if maxIter < 1 {
		maxIter = maxDefault
	}

	// 6) Optional integration credentials (blank to skip). These enable the
	// web_search, github, and notify tools respectively.
	fmt.Fprint(out, "Brave Search API key (optional): ")
	brave, err := readLine()
	if err != nil {
		return Result{}, err
	}
	fmt.Fprint(out, "GitHub token (optional): ")
	ghToken, err := readLine()
	if err != nil {
		return Result{}, err
	}
	fmt.Fprint(out, "Slack/Discord notify webhook URL (optional): ")
	notify, err := readLine()
	if err != nil {
		return Result{}, err
	}

	return Result{
		BaseURL: baseURL, ChatPath: chatPath, ModelsPath: modelsPath,
		APIKey: apiKey, Model: model, ApprovalMode: mode, MaxIter: maxIter,
		BraveAPIKey: strings.TrimSpace(brave), GitHubToken: strings.TrimSpace(ghToken), NotifyWebhook: strings.TrimSpace(notify),
	}, nil
}

func listModels(opts Options, baseURL, modelsPath, apiKey string) ([]string, error) {
	if opts.ListModels == nil {
		return nil, nil
	}
	return opts.ListModels(baseURL, modelsPath, apiKey)
}

// NeedsSetup reports whether the first-run wizard should trigger: only when the
// session is interactive, no saved config exists, and no base URL was supplied
// by any other means (real env, a .env, or a flag).
func NeedsSetup(baseURLProvided, globalConfigExists, interactive bool) bool {
	return interactive && !globalConfigExists && !baseURLProvided
}

func parseIntOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func defaultHint(def string) string {
	if def == "" {
		return ""
	}
	return " [" + def + "]"
}
