package planner

import (
	"strings"
	"testing"
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

// A pure node never does I/O, so a URL in its test is data: run
// linkcheck-v1-20261104-r002 spent five rounds on fn-calculate-fixed-count
// and twelve on fn-classify-targets (both pure) because the model kept
// a.com and example.com in []types.Result rows. Every other class keeps
// the rule: the cli.Run handler of linkcheck-v1-20261104-r001 fetched
// example.com for real.
func TestHostRuleAppliesToEveryClassButPure(t *testing.T) {
	if hostRuleApplies("pure") {
		t.Error("the host rule applies to a pure node")
	}
	for _, class := range []string{"handler", "client", "wiring", "validation", "storage", "concurrency", "other", ""} {
		if !hostRuleApplies(class) {
			t.Errorf("the host rule skips class %q", class)
		}
	}
}
