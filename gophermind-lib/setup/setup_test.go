package setup

import (
	"errors"
	"strings"
	"testing"
)

// builtins is a stand-in endpoint menu for the wizard tests. The third and
// fourth elements (chatPath, modelsPath) are empty for both, matching
// neither entry needing a non-default path in these fixtures.
var builtins = [][4]string{
	{"local-llama", "http://127.0.0.1:8080", "", ""},
	{"openai", "https://api.openai.com/v1", "", ""},
}

func TestRunCustomEndpointPicksDiscoveredModel(t *testing.T) {
	// choice 3 = custom, then URL, key, model pick #2, approval auto, max-iter 40.
	in := "3\nhttp://x:8000\nsecret\n2\nauto\n40\n"
	opts := Options{
		In:       strings.NewReader(in),
		Out:      &strings.Builder{},
		Profiles: builtins,
		ListModels: func(baseURL, modelsPath, apiKey string) ([]string, error) {
			if baseURL != "http://x:8000" || apiKey != "secret" {
				t.Errorf("ListModels got baseURL=%q apiKey=%q", baseURL, apiKey)
			}
			return []string{"m1", "m2"}, nil
		},
	}
	got, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := Result{BaseURL: "http://x:8000", APIKey: "secret", Model: "m2", ApprovalMode: "auto", MaxIter: 40}
	if got != want {
		t.Errorf("Run = %+v, want %+v", got, want)
	}
}

func TestRunMaxIterBlankUsesDefault(t *testing.T) {
	// Blank max-iter answer falls back to Defaults.MaxIter.
	in := "1\n\n1\nask\n\n"
	opts := Options{
		In:         strings.NewReader(in),
		Out:        &strings.Builder{},
		Profiles:   builtins,
		ListModels: func(baseURL, modelsPath, apiKey string) ([]string, error) { return []string{"m"}, nil },
		Defaults:   Result{MaxIter: 25},
	}
	got, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.MaxIter != 25 {
		t.Errorf("MaxIter = %d, want 25 (default)", got.MaxIter)
	}
}

func TestRunBuiltinProfileBlankKeyDefaultApproval(t *testing.T) {
	// choice 1 = local-llama (no URL prompt), blank key, model pick #1, blank approval.
	in := "1\n\n1\n\n"
	opts := Options{
		In:         strings.NewReader(in),
		Out:        &strings.Builder{},
		Profiles:   builtins,
		ListModels: func(baseURL, modelsPath, apiKey string) ([]string, error) { return []string{"only-model"}, nil },
	}
	got, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// No max-iter answer given (EOF) and no Defaults.MaxIter => built-in default 25.
	want := Result{BaseURL: "http://127.0.0.1:8080", APIKey: "", Model: "only-model", ApprovalMode: "ask", MaxIter: 25}
	if got != want {
		t.Errorf("Run = %+v, want %+v", got, want)
	}
}

func TestRunModelDiscoveryFailureFallsBackToFreeText(t *testing.T) {
	// custom endpoint, key, discovery fails -> type a model name, approval ask.
	in := "3\nhttp://x\nk\nqwen3.6-35b-a3b\nask\n"
	opts := Options{
		In:         strings.NewReader(in),
		Out:        &strings.Builder{},
		Profiles:   builtins,
		ListModels: func(baseURL, modelsPath, apiKey string) ([]string, error) { return nil, errors.New("unreachable") },
	}
	got, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Model != "qwen3.6-35b-a3b" {
		t.Errorf("Model = %q, want qwen3.6-35b-a3b", got.Model)
	}
	if got.BaseURL != "http://x" {
		t.Errorf("BaseURL = %q, want http://x", got.BaseURL)
	}
}

// TestRunProfileChatPathFlowsToResult is the regression test for the wizard
// bug where picking a menu profile whose base URL already ends in /v1 (e.g.
// openai) still produced an empty ChatPath/ModelsPath and therefore a
// doubled /v1/v1/chat/completions (and /v1/v1/models) once the saved config
// was used. Selecting the profile must carry its chatPath/modelsPath into
// the Result, AND (the model-picker half of the same bug) into the
// ListModels probe itself: ListModels used to be called with no modelsPath
// at all, so it always probed the client's bare default and 404d against any
// profile whose BaseURL already ends in /v1.
func TestRunProfileChatPathFlowsToResult(t *testing.T) {
	profiles := [][4]string{
		{"local-llama", "http://127.0.0.1:8080", "", ""},
		{"openai", "https://api.openai.com/v1", "/chat/completions", "/models"},
	}
	// choice 2 = openai (no URL prompt), blank key, model pick #1, blank approval.
	in := "2\n\n1\n\n"
	opts := Options{
		In:       strings.NewReader(in),
		Out:      &strings.Builder{},
		Profiles: profiles,
		ListModels: func(baseURL, modelsPath, apiKey string) ([]string, error) {
			if modelsPath != "/models" {
				t.Errorf("ListModels got modelsPath=%q, want /models from the chosen profile", modelsPath)
			}
			return []string{"m"}, nil
		},
	}
	got, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.ChatPath != "/chat/completions" {
		t.Errorf("ChatPath = %q, want /chat/completions from the chosen profile", got.ChatPath)
	}
	if got.ModelsPath != "/models" {
		t.Errorf("ModelsPath = %q, want /models from the chosen profile", got.ModelsPath)
	}
	pairs := map[string]string{}
	for _, p := range got.Pairs() {
		pairs[p[0]] = p[1]
	}
	if pairs["GOPHERMIND_CHAT_PATH"] != "/chat/completions" {
		t.Errorf("Pairs() GOPHERMIND_CHAT_PATH = %q, want /chat/completions: %v", pairs["GOPHERMIND_CHAT_PATH"], got.Pairs())
	}
	if pairs["GOPHERMIND_MODELS_PATH"] != "/models" {
		t.Errorf("Pairs() GOPHERMIND_MODELS_PATH = %q, want /models: %v", pairs["GOPHERMIND_MODELS_PATH"], got.Pairs())
	}
}

// A custom URL carries no known chat/models path: they stay empty (the
// client defaults), matching the fields' no-op-by-default design.
func TestRunCustomEndpointChatPathStaysEmpty(t *testing.T) {
	in := "3\nhttp://x:8000\n\nm\nask\n"
	opts := Options{
		In:         strings.NewReader(in),
		Out:        &strings.Builder{},
		Profiles:   builtins,
		ListModels: func(baseURL, modelsPath, apiKey string) ([]string, error) { return nil, errors.New("unreachable") },
	}
	got, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.ChatPath != "" {
		t.Errorf("ChatPath = %q, want empty for a custom URL", got.ChatPath)
	}
	if got.ModelsPath != "" {
		t.Errorf("ModelsPath = %q, want empty for a custom URL", got.ModelsPath)
	}
}

// TestRunSwitchingProfileClearsStaleChatPath is the regression test for item
// C: picking a profile that sets a chat path, then re-running the wizard and
// picking one that does not, must emit an EXPLICIT empty pair for
// GOPHERMIND_CHAT_PATH/GOPHERMIND_MODELS_PATH so config.Save deletes the
// stale key instead of leaving it in place (which would otherwise make
// local-llama resolve to .../8080/chat/completions and 404).
func TestRunSwitchingProfileClearsStaleChatPath(t *testing.T) {
	profiles := [][4]string{
		{"local-llama", "http://127.0.0.1:8080", "", ""},
		{"openai", "https://api.openai.com/v1", "/chat/completions", "/models"},
	}

	// First run: pick openai, which sets both paths.
	first, err := Run(Options{
		In:         strings.NewReader("2\n\n1\n\n"),
		Out:        &strings.Builder{},
		Profiles:   profiles,
		ListModels: func(baseURL, modelsPath, apiKey string) ([]string, error) { return []string{"m"}, nil },
	})
	if err != nil {
		t.Fatalf("Run (openai): %v", err)
	}
	if first.ChatPath == "" || first.ModelsPath == "" {
		t.Fatalf("first run did not set paths: %+v", first)
	}

	// Second run: pick local-llama, which sets neither. Defaults carries the
	// prior run's values forward the way the TUI/CLI wizards do (pre-filling
	// from the current config), so this proves the menu selection itself
	// clears the paths rather than Defaults leaking through.
	second, err := Run(Options{
		In:         strings.NewReader("1\n\n1\n\n"),
		Out:        &strings.Builder{},
		Profiles:   profiles,
		ListModels: func(baseURL, modelsPath, apiKey string) ([]string, error) { return []string{"m"}, nil },
		Defaults:   Result{ChatPath: first.ChatPath, ModelsPath: first.ModelsPath},
	})
	if err != nil {
		t.Fatalf("Run (local-llama): %v", err)
	}
	if second.ChatPath != "" {
		t.Errorf("ChatPath = %q, want empty after switching to local-llama", second.ChatPath)
	}
	if second.ModelsPath != "" {
		t.Errorf("ModelsPath = %q, want empty after switching to local-llama", second.ModelsPath)
	}

	// The key proof: Pairs() must carry an EXPLICIT empty entry for both,
	// not omit them, so config.Save's "empty value deletes the key" rule
	// actually fires and removes the stale chat_path/models_path.
	pairs := map[string]string{}
	present := map[string]bool{}
	for _, p := range second.Pairs() {
		pairs[p[0]] = p[1]
		present[p[0]] = true
	}
	if !present["GOPHERMIND_CHAT_PATH"] || pairs["GOPHERMIND_CHAT_PATH"] != "" {
		t.Errorf("Pairs() must carry an explicit empty GOPHERMIND_CHAT_PATH to clear the stale key, got %v", second.Pairs())
	}
	if !present["GOPHERMIND_MODELS_PATH"] || pairs["GOPHERMIND_MODELS_PATH"] != "" {
		t.Errorf("Pairs() must carry an explicit empty GOPHERMIND_MODELS_PATH to clear the stale key, got %v", second.Pairs())
	}
}

func TestResultPairsOmitsEmptyOptionalValues(t *testing.T) {
	r := Result{BaseURL: "http://x", APIKey: "", Model: "", ApprovalMode: "ask"}
	pairs := r.Pairs()
	got := map[string]string{}
	for _, p := range pairs {
		got[p[0]] = p[1]
	}
	if got["GOPHERMIND_BASE_URL"] != "http://x" {
		t.Errorf("base url pair = %q", got["GOPHERMIND_BASE_URL"])
	}
	if got["GOPHERMIND_APPROVAL"] != "ask" {
		t.Errorf("approval pair = %q", got["GOPHERMIND_APPROVAL"])
	}
	if _, ok := got["GOPHERMIND_API_KEY"]; ok {
		t.Errorf("empty API key should be omitted, got pairs %v", pairs)
	}
	if _, ok := got["GOPHERMIND_MODEL"]; ok {
		t.Errorf("empty model should be omitted, got pairs %v", pairs)
	}
}

func TestResultPairsIncludesKeyModelAndMaxIterWhenSet(t *testing.T) {
	r := Result{BaseURL: "http://x", APIKey: "sk-1", Model: "m", ApprovalMode: "auto", MaxIter: 40}
	got := map[string]string{}
	for _, p := range r.Pairs() {
		got[p[0]] = p[1]
	}
	if got["GOPHERMIND_API_KEY"] != "sk-1" || got["GOPHERMIND_MODEL"] != "m" {
		t.Errorf("pairs missing key/model: %v", got)
	}
	if got["GOPHERMIND_MAX_ITER"] != "40" {
		t.Errorf("GOPHERMIND_MAX_ITER = %q, want 40", got["GOPHERMIND_MAX_ITER"])
	}
}

func TestResultPairsIncludesSpeedModelWhenSet(t *testing.T) {
	r := Result{BaseURL: "http://x", ApprovalMode: "ask", SpeedModel: "fast-tier"}
	got := map[string]string{}
	for _, p := range r.Pairs() {
		got[p[0]] = p[1]
	}
	if got["GOPHERMIND_SPEED_MODEL"] != "fast-tier" {
		t.Errorf("GOPHERMIND_SPEED_MODEL = %q, want fast-tier", got["GOPHERMIND_SPEED_MODEL"])
	}
}

func TestResultPairsOmitsEmptySpeedModel(t *testing.T) {
	r := Result{BaseURL: "http://x", ApprovalMode: "ask"}
	for _, p := range r.Pairs() {
		if p[0] == "GOPHERMIND_SPEED_MODEL" {
			t.Errorf("empty speed model should be omitted, got %v", p)
		}
	}
}

func TestResultPairsOmitsZeroMaxIter(t *testing.T) {
	r := Result{BaseURL: "http://x", ApprovalMode: "ask"} // MaxIter 0
	for _, p := range r.Pairs() {
		if p[0] == "GOPHERMIND_MAX_ITER" {
			t.Errorf("zero MaxIter should be omitted, got %v", p)
		}
	}
}

func TestNeedsSetup(t *testing.T) {
	cases := []struct {
		name                                          string
		baseProvided, globalExists, interactive, want bool
	}{
		{"fresh interactive -> yes", false, false, true, true},
		{"not a tty -> no", false, false, false, false},
		{"already configured file -> no", false, true, true, false},
		{"base url provided -> no", true, false, true, false},
	}
	for _, c := range cases {
		if got := NeedsSetup(c.baseProvided, c.globalExists, c.interactive); got != c.want {
			t.Errorf("%s: NeedsSetup(%v,%v,%v) = %v, want %v", c.name, c.baseProvided, c.globalExists, c.interactive, got, c.want)
		}
	}
}

// Persistence perms/format are config.Save's concern now; see
// internal/config/file_test.go.

func TestPairsIncludesIntegrations(t *testing.T) {
	r := Result{BaseURL: "http://x", ApprovalMode: "ask", BraveAPIKey: "bk", GitHubToken: "gh", NotifyWebhook: "http://hook"}
	pairs := r.Pairs()
	got := map[string]string{}
	for _, p := range pairs {
		got[p[0]] = p[1]
	}
	if got["GOPHERMIND_BRAVE_API_KEY"] != "bk" || got["GITHUB_TOKEN"] != "gh" || got["GOPHERMIND_NOTIFY_WEBHOOK"] != "http://hook" {
		t.Errorf("integration pairs missing: %v", got)
	}
	// Empty integrations are omitted.
	empty := Result{BaseURL: "http://x", ApprovalMode: "ask"}.Pairs()
	for _, p := range empty {
		if p[0] == "GITHUB_TOKEN" || p[0] == "GOPHERMIND_BRAVE_API_KEY" {
			t.Errorf("empty integration should be omitted, got %v", p)
		}
	}
}
