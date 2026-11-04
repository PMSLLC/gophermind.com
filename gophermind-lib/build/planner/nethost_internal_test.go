package planner

import (
	"strings"
	"testing"

	"gophermind/gophermind-lib/build/contract"
)

func hostSrc(body string) string {
	return "package cli_test\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) {\n\t" + body + "\n}\n"
}

func TestCheckNoRealHostRefusesALiteralHost(t *testing.T) {
	for _, c := range []struct{ name, body, host string }{
		{"https host", `_ = "https://example.com/redir"`, "example.com"},
		{"http host with a port", `_ = "http://this-does-not-exist-12345.com:8080/x"`, "this-does-not-exist-12345.com:8080"},
		{"upper-case scheme", `_ = "HTTP://example.com/x"`, "example.com"},
		{"private ip", `_ = "http://10.0.0.1/x"`, "10.0.0.1"},
		{"raw string fixture", "_ = `- [a](http://example.org/a)`", "example.org"},
	} {
		err := checkNoRealHost(hostSrc(c.body))
		if err == nil || !strings.Contains(err.Error(), "line 6") || !strings.Contains(err.Error(), c.host) || !strings.Contains(err.Error(), "srv.URL") {
			t.Errorf("%s: err = %v, want a refusal naming line 6, %s and srv.URL", c.name, err, c.host)
		}
	}
}

func TestCheckNoRealHostAcceptsLoopbackFormatVerbsAndComments(t *testing.T) {
	for _, body := range []string{
		`_ = "http://127.0.0.1:8080/x"`,
		`_ = "http://localhost/x"`,
		`_ = "http://[::1]/x"`,
		"_ = `[a](http://127.0.0.1:8080)`",
		"_ = `<a href=http://localhost>`",
		`_ = "http://%s/a"`,
		`_ = "http://" + "x"`,
		`srv := "x"; _ = srv + "/a"`,
		`// see http://example.com/docs`,
		`_ = "no scheme here example.com"`,
	} {
		if err := checkNoRealHost(hostSrc(body)); err != nil {
			t.Errorf("%s: refused: %v", body, err)
		}
	}
}

// Link extraction and classification need URL data that is never fetched.
// Reserved top-level names (RFC 2606, RFC 6761) never resolve, so they are
// safe; example.com does resolve and is exactly what call 113 fetched.
func TestCheckNoRealHostAcceptsReservedNames(t *testing.T) {
	for _, body := range []string{
		`_ = "http://a.invalid/x"`,
		`_ = "http://host.test/x"`,
		`_ = "https://docs.example/a"`,
		`_ = "http://app.localhost:8080/x"`,
		"_ = `[a](http://A.INVALID)`",
	} {
		if err := checkNoRealHost(hostSrc(body)); err != nil {
			t.Errorf("%s: refused: %v", body, err)
		}
	}
	err := checkNoRealHost(hostSrc(`_ = "http://example.com/x"`))
	if err == nil || !strings.Contains(err.Error(), ".invalid") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("example.com: err = %v, want a refusal that suggests .invalid and not 127.0.0.1", err)
	}
}

// A URL in the test of a node that cannot reach the network is data: run
// linkcheck-v1-20261104-r002 spent five rounds on calculateFixedCount, twelve
// on ClassifyTargets and more on Extract(r io.Reader) because Gemma kept
// a.com and example.com in fixture rows. The node class is the model's guess
// (it called Extract a client), so the contract's uses graph decides: cli.Run
// reaches check.One, whose package holds resolveURL(*http.Client), and its
// test fetched example.com for real in run r001.
func TestReachesNetworkFollowsTheUsesGraph(t *testing.T) {
	c := &contract.Contracts{Functions: []contract.Function{
		{ID: "fn-run", Package: "cli", Signature: "func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int", Uses: []string{"fn-parse-flags", "fn-process-file"}},
		{ID: "fn-process-file", Package: "cli", Signature: "func processFile(path string) int", Uses: []string{"fn-extract-links", "fn-one", "fn-run"}},
		{ID: "fn-parse-flags", Package: "cli", Signature: "func parseFlags(args []string) (config, error)"},
		{ID: "fn-one", Package: "check", Signature: "func One(url string, cfg CheckConfig) types.Result", Uses: []string{"result"}},
		{ID: "fn-resolve-url", Package: "check", Signature: "func resolveURL(url string, client *http.Client) (string, error)"},
		{ID: "fn-extract-links", Package: "links", Signature: "func Extract(r io.Reader) ([]types.Link, error)", Uses: []string{"link"}},
		{ID: "fn-count", Package: "cli", Signature: "func count(results []types.Result) int", Uses: []string{"result"}},
	}}
	for id, want := range map[string]bool{"fn-run": true, "fn-one": true, "fn-resolve-url": true, "fn-extract-links": false, "fn-count": false, "fn-parse-flags": false, "fn-unknown": true} {
		if got := reachesNetwork(c, id); got != want {
			t.Errorf("reachesNetwork(%s) = %v, want %v", id, got, want)
		}
	}
	if !reachesNetwork(nil, "fn-count") {
		t.Error("no contract: the rule must stay on")
	}
}

func TestImportsNetSeesTheTestFileItself(t *testing.T) {
	cases := map[string]bool{
		"package p\n\nimport \"net/http\"\n\nvar _ = http.Get\n":                            true,
		"package p\n\nimport \"net\"\n\nvar _ = net.Dial\n":                                 true,
		"package p\n\nimport (\n\t\"net/http/httptest\"\n\t\"net/url\"\n\t\"testing\"\n)\n": false,
		"package p\n\nimport \"strings\"\n":                                                 false,
		"not go":                                                                            false,
	}
	for src, want := range cases {
		if got := importsNet(src); got != want {
			t.Errorf("importsNet(%q) = %v, want %v", src, got, want)
		}
	}
}
