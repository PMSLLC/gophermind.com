// Package setup implements gophermind's first-run configuration wizard: an
// interactive prompt sequence that captures the essentials (endpoint, API key,
// model, approval mode) and persists them as GOPHERMIND_* pairs to a .env file
// that config loading already reads. It has no dependency on the llm client or
// TUI: model discovery and secret reading are injected, so the flow is fully
// testable without a terminal or a live endpoint.
package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
}

// Pairs renders the result as ordered GOPHERMIND_* env pairs for persistence.
// Empty optional values (API key, model) are omitted so a blank answer never
// writes a spurious line.
func (r Result) Pairs() [][2]string {
	pairs := [][2]string{{"GOPHERMIND_BASE_URL", r.BaseURL}}
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
	return pairs
}

// Options configures a wizard run. In/Out are the I/O streams; Profiles is the
// endpoint menu ({name, baseURL}); ListModels fetches selectable models for the
// chosen endpoint; ReadSecret reads the API key without echo (nil => read a
// plain line from In); Defaults pre-fills answers when re-running.
type Options struct {
	In         io.Reader
	Out        io.Writer
	Profiles   [][2]string
	ListModels func(baseURL, apiKey string) ([]string, error)
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

	var baseURL string
	if choice >= 1 && choice <= len(opts.Profiles) {
		baseURL = opts.Profiles[choice-1][1]
	} else {
		// Custom (or out-of-range): prompt for a URL, defaulting to any prior value.
		def := opts.Defaults.BaseURL
		fmt.Fprintf(out, "Base URL%s: ", defaultHint(def))
		line, err := readLine()
		if err != nil {
			return Result{}, err
		}
		baseURL = firstNonEmpty(strings.TrimSpace(line), def)
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
	models, listErr := listModels(opts, baseURL, apiKey)
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

	return Result{BaseURL: baseURL, APIKey: apiKey, Model: model, ApprovalMode: mode, MaxIter: maxIter}, nil
}

func listModels(opts Options, baseURL, apiKey string) ([]string, error) {
	if opts.ListModels == nil {
		return nil, nil
	}
	return opts.ListModels(baseURL, apiKey)
}

// NeedsSetup reports whether the first-run wizard should trigger: only when the
// session is interactive, no saved config exists, and no base URL was supplied
// by any other means (real env, a .env, or a flag).
func NeedsSetup(baseURLProvided, globalConfigExists, interactive bool) bool {
	return interactive && !globalConfigExists && !baseURLProvided
}

// WriteEnv atomically writes GOPHERMIND_* pairs to a .env file at path, creating
// the parent directory 0700 and the file 0600 (it may contain an API key).
func WriteEnv(path string, pairs [][2]string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	var b strings.Builder
	for _, p := range pairs {
		fmt.Fprintf(&b, "%s=%s\n", p[0], quoteEnvValue(p[1]))
	}
	tmp, err := os.CreateTemp(dir, ".env-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// quoteEnvValue wraps a value in double quotes when it contains whitespace, a
// '#', or is empty, matching the subset that config's .env loader unquotes.
func quoteEnvValue(v string) string {
	if v == "" || strings.ContainsAny(v, " \t#\"'") {
		return `"` + v + `"`
	}
	return v
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
