package setup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// builtins is a stand-in endpoint menu for the wizard tests.
var builtins = [][2]string{
	{"local-llama", "http://127.0.0.1:8080"},
	{"openai", "https://api.openai.com/v1"},
}

func TestRunCustomEndpointPicksDiscoveredModel(t *testing.T) {
	// choice 3 = custom, then URL, key, model pick #2, approval auto, max-iter 40.
	in := "3\nhttp://x:8000\nsecret\n2\nauto\n40\n"
	opts := Options{
		In:       strings.NewReader(in),
		Out:      &strings.Builder{},
		Profiles: builtins,
		ListModels: func(baseURL, apiKey string) ([]string, error) {
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
		ListModels: func(baseURL, apiKey string) ([]string, error) { return []string{"m"}, nil },
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
		ListModels: func(baseURL, apiKey string) ([]string, error) { return []string{"only-model"}, nil },
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
		ListModels: func(baseURL, apiKey string) ([]string, error) { return nil, errors.New("unreachable") },
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

func TestWriteEnvPermsAndQuoting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg", ".env")
	pairs := [][2]string{
		{"GOPHERMIND_BASE_URL", "http://x:8000"},
		{"GOPHERMIND_MODEL", "qwen 3.6"}, // space must be quoted
	}
	if err := WriteEnv(path, pairs); err != nil {
		t.Fatalf("WriteEnv: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file perm = %o, want 600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Join(dir, "cfg"))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir perm = %o, want 700", di.Mode().Perm())
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, "GOPHERMIND_BASE_URL=http://x:8000") {
		t.Errorf("missing base url line:\n%s", content)
	}
	if !strings.Contains(content, `GOPHERMIND_MODEL="qwen 3.6"`) {
		t.Errorf("model with space should be quoted:\n%s", content)
	}
}
