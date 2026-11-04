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
