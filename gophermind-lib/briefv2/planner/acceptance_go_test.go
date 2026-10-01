package planner_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/planner"
)

const acceptBullet = "A round trip: `GET /hello` through the running server returns 200 and `Hello`."

// narrativeBrief adds a narrative acceptance bullet to the greeter brief.
func narrativeBrief(s string) string {
	return strings.TrimRight(s, "\n") + "\n- " + acceptBullet + "\n"
}

const acceptSource = `package acceptance

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestA4(t *testing.T) {
	base := os.Getenv("GM_ACCEPTANCE_URL")
	if base == "" {
		t.Fatal("GM_ACCEPTANCE_URL is not set")
	}
	resp, err := http.Get(base + "/hello")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "Hello") {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
`

// goAcceptRig plans the greeter brief with a narrative bullet and a serve
// declaration: the coverage reply gives it a (weak) shell command that the
// harness replaces, and the Test-writer replies with a Go test.
func goAcceptRig(t *testing.T, serve bool, extra map[string]string) *rig {
	t.Helper()
	cov := string(mustRead(t, filepath.Join(greeter, "coverage.txt")))
	cov = strings.Replace(cov, `{"requirement": "A3", "nodes": ["fn-greet", "fn-farewell"]}`,
		`{"requirement": "A3", "nodes": ["fn-greet", "fn-farewell"]}, {"requirement": "A4", "nodes": ["fn-greet"]}`, 1)
	cov = strings.Replace(cov, `"root_tests": [`, `"root_tests": [
    {"requirement": "A4", "name": "round trip", "given": "the server runs", "expect": "200 and Hello", "command": "curl -fsS \"$GM_ACCEPTANCE_URL/hello\" | grep -q Hello"},`, 1)
	if serve {
		cov = strings.Replace(cov, `"root_tests": [`, `"serve": {"command": "greeter --addr \"$GM_ACCEPTANCE_ADDR\"", "ready": "/healthz"}, "root_tests": [`, 1)
	}
	files := map[string]string{"coverage.txt": cov, "testwrite.accept-A4.txt": `{"test_file": ` + jsonString(acceptSource) + `}`}
	for k, v := range extra {
		files[k] = v
	}
	g := newRig(t, approving(), variant(t, files))
	g.briefPath = writeBrief(t, g.repo, narrativeBrief)
	return g
}

func TestTestwriterWritesAGoAcceptanceTest(t *testing.T) {
	g := goAcceptRig(t, true, nil)
	g.mustPlan(planner.Options{})

	cov, err := planner.ReadCoverage(g.runDir)
	if err != nil || cov.Serve == nil || cov.Serve.Ready != "/healthz" {
		t.Fatalf("coverage = %+v, %v", cov, err)
	}
	want := "go test -tags acceptance ./acceptance -run '^TestA4$' -count=1 -v"
	var found bool
	for _, rt := range cov.RootTests {
		if rt.Requirement == "A4" {
			found = rt.Command == want
		}
	}
	if !found {
		t.Errorf("the root test of A4 is not %q: %+v", want, cov.RootTests)
	}

	path := filepath.Join(g.repo, "acceptance", "a4_test.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the acceptance test was not written: %v", err)
	}
	if !strings.HasPrefix(string(raw), "//go:build acceptance\n\npackage acceptance\n") {
		t.Errorf("the file lacks the build tag:\n%.100s", raw)
	}
	tests, err := planner.ReadAcceptanceTests(g.runDir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	at, ok := tests["A4"]
	if !ok || at.TestFile != "acceptance/a4_test.go" || at.TestFunc != "TestA4" || at.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("manifest = %+v", tests)
	}
	var files []string
	if err := json.Unmarshal(g.read("_state/test_files.json"), &files); err != nil || !strings.Contains(strings.Join(files, " "), "acceptance/a4_test.go") {
		t.Errorf("test_files.json = %v, %v", files, err)
	}
	var prompt string
	for _, r := range g.fake.Requests() {
		if planner.StageOf(r) == "testwrite:accept-A4" {
			prompt = r.Messages[1].Content
		}
	}
	for _, want := range []string{"A4", "TestA4", "GM_ACCEPTANCE_URL", "A round trip", "acceptance/a4_test.go"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the acceptance test prompt lacks %q", want)
		}
	}
}

// Without a server declared the narrative bullet keeps its shell command and no
// Go test is written.
func TestNarrativeBulletWithoutServeStaysShell(t *testing.T) {
	g := goAcceptRig(t, false, nil)
	g.mustPlan(planner.Options{})
	if _, err := os.Stat(filepath.Join(g.repo, "acceptance")); err == nil {
		t.Error("an acceptance folder was written without serve")
	}
	tests, err := planner.ReadAcceptanceTests(g.runDir)
	if err != nil || len(tests) != 0 {
		t.Errorf("manifest = %+v, %v", tests, err)
	}
}

func TestAcceptanceTestReplyIsRetriedAndNeverQuoted(t *testing.T) {
	bad := `{"test_file": ` + jsonString(strings.Replace(acceptSource, `t.Fatal("GM_ACCEPTANCE_URL is not set")`, `t.Skip("REPLYCANARY")`, 1)) + `}`
	g := goAcceptRig(t, true, map[string]string{"testwrite.accept-A4.txt": bad, "testwrite.accept-A4.2.txt": `{"test_file": ` + jsonString(acceptSource) + `}`})
	g.mustPlan(planner.Options{})
	if n := count(g.stagesCalled(), "testwrite:accept-A4"); n != 2 {
		t.Errorf("calls = %d, want a retry", n)
	}
}

func TestAcceptanceTestFileThatAlreadyExistsIsRefused(t *testing.T) {
	g := goAcceptRig(t, true, nil)
	if err := os.MkdirAll(filepath.Join(g.repo, "acceptance"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.repo, "acceptance", "a4_test.go"), []byte("package acceptance\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := g.plan(planner.Options{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
}
