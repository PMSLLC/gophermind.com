package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"gophermind/gophermind-lib/safety"
)

const burrowTestToken = "burrow-test-token-value"

// burrowToolEnv starts a fake Burrow answering every lookup with status/body
// and returns a chatTools wired to it plus the last request body it saw.
func burrowToolEnv(t *testing.T, status int, body string) (*chatTools, *string) {
	t.Helper()
	var last string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 1<<16)
		n, _ := r.Body.Read(b)
		last = string(b[:n])
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	tf := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(tf, []byte(burrowTestToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &chatTools{burrow: newBurrowClient(srv.URL, tf)}, &last
}

func runBurrow(t *testing.T, ct *chatTools, args string) (string, error) {
	t.Helper()
	for _, tl := range ct.all() {
		if tl.Name == "burrow_lookup" {
			return tl.Run(context.Background(), json.RawMessage(args))
		}
	}
	t.Fatal("burrow_lookup not registered")
	return "", nil
}

const burrowGoodArgs = `{"verb":"Count","object":"Text.Words","inputs":[{"name":"text","type":"string"}],"outputs":["int64"]}`

func burrowHitBody(body string, awaiting bool) string {
	b, _ := json.Marshal(map[string]any{
		"outcome":  "exact",
		"key":      "text/words/count@d10b1d2a09aa",
		"contract": json.RawMessage(`{"verb":"Count","object":"Text.Words","inputs":[{"name":"b","type":"string"},{"name":"a","type":"int64"}],"outputs":["int64"],"effects":[],"traits":[],"failures":[]}`),
		"rendering": map[string]any{
			"lang": "go", "version": 3, "body": body, "verified_at": "2026-10-25T18:30:00Z",
		},
		"eligible":              true,
		"awaiting_confirmation": awaiting,
	})
	return string(b)
}

func TestBurrowLookupToolHit(t *testing.T) {
	ct, last := burrowToolEnv(t, 200, burrowHitBody("package main\nfunc Run() {}\n", false))
	out, err := runBurrow(t, ct, burrowGoodArgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"found: yes (exact)", "text/words/count@d10b1d2a09aa", "parameter order: b, a", "verified at: 2026-10-25T18:30:00Z", "language: go", "eligible: true", "func Run() {}"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(*last, `"lang":"go"`) {
		t.Errorf("lang default not sent: %s", *last)
	}
	for _, w := range []string{`"failures":[]`, `"traits":[]`, `"effects":[]`} {
		if !strings.Contains(*last, w) {
			t.Errorf("request lacks %s: %s", w, *last)
		}
	}
	if strings.Contains(out, burrowTestToken) {
		t.Error("token leaked")
	}
}

func TestBurrowLookupToolMiss(t *testing.T) {
	ct, _ := burrowToolEnv(t, 200, `{"outcome":"miss","key":"k","contract":null,"rendering":null,"eligible":true,"awaiting_confirmation":false}`)
	out, err := runBurrow(t, ct, burrowGoodArgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "found: no (exact contract only; similar functions are not searched without test vectors)") {
		t.Errorf("miss text: %s", out)
	}
	if strings.Contains(out, "parameter order") {
		t.Errorf("miss must not show parameter order: %s", out)
	}
}

func TestBurrowLookupToolAwaitingConfirmation(t *testing.T) {
	ct, _ := burrowToolEnv(t, 200, `{"outcome":"miss","key":"k","contract":null,"rendering":null,"eligible":true,"awaiting_confirmation":true}`)
	out, _ := runBurrow(t, ct, burrowGoodArgs)
	if !strings.Contains(out, "not confirmed") || !strings.Contains(out, "not shown") {
		t.Errorf("awaiting text: %s", out)
	}
	ct, _ = burrowToolEnv(t, 200, burrowHitBody("SECRETBODY", true))
	out, _ = runBurrow(t, ct, burrowGoodArgs)
	if strings.Contains(out, "SECRETBODY") || !strings.Contains(out, "not confirmed") {
		t.Errorf("unconfirmed body must be hidden: %s", out)
	}
}

func TestBurrowLookupToolBodyCapped(t *testing.T) {
	big := "package main\n" + strings.Repeat("// padding line\n", 3000)
	ct, _ := burrowToolEnv(t, 200, burrowHitBody(big, false))
	out, err := runBurrow(t, ct, burrowGoodArgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > maxToolOutput {
		t.Errorf("output %d bytes, want <= %d", len(out), maxToolOutput)
	}
	for _, want := range []string{"text/words/count@d10b1d2a09aa", "parameter order: b, a", "found: yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("capped output lost %q", want)
		}
	}
}

func TestBurrowLookupToolErrorSentences(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{401, `{}`, "token was rejected"},
		{400, `{"error":{"message":"bad type zzz"}}`, "bad type zzz"},
		{500, `{}`, "failed"},
		{404, `{}`, "Burrow does not know that route/version"},
		{429, `{}`, "Burrow is busy, try again"},
		{418, `{}`, "Burrow lookup failed"},
	}
	for _, c := range cases {
		ct, _ := burrowToolEnv(t, c.status, c.body)
		out, err := runBurrow(t, ct, burrowGoodArgs)
		if err != nil || !strings.Contains(out, c.want) {
			t.Errorf("status %d: got %q, %v; want %q", c.status, out, err, c.want)
		}
	}
	// unreachable: a server that is closed.
	ct, _ := burrowToolEnv(t, 200, `{}`)
	ct.burrow.baseURL = "http://127.0.0.1:1"
	out, err := runBurrow(t, ct, burrowGoodArgs)
	if err != nil || !strings.Contains(out, "did not answer") {
		t.Errorf("unreachable: %q, %v", out, err)
	}
}

func TestBurrowLookupToolRejectsBadArgs(t *testing.T) {
	ct, last := burrowToolEnv(t, 200, `{"outcome":"miss"}`)
	bad := map[string]string{
		"case dup":      `{"verb":"Count","Verb":"Other","object":"T","inputs":[],"outputs":["int64"]}`,
		"unknown key":   `{"verb":"Count","object":"T","inputs":[],"outputs":["int64"],"vectors":[]}`,
		"empty verb":    `{"verb":" ","object":"T","inputs":[],"outputs":["int64"]}`,
		"missing outs":  `{"verb":"Count","object":"T","inputs":[]}`,
		"empty outs":    `{"verb":"Count","object":"T","inputs":[],"outputs":[]}`,
		"input no name": `{"verb":"Count","object":"T","inputs":[{"type":"string"}],"outputs":["int64"]}`,
		"input extra":   `{"verb":"Count","object":"T","inputs":[{"name":"a","type":"string","x":1}],"outputs":["int64"]}`,
		"not json":      `nope`,
	}
	for name, args := range bad {
		if _, err := runBurrow(t, ct, args); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if *last != "" {
		t.Errorf("bad args reached Burrow: %s", *last)
	}
}

func TestBurrowLookupToolNotRegisteredWithoutConfig(t *testing.T) {
	ct := &chatTools{}
	for _, tl := range ct.all() {
		if tl.Name == "burrow_lookup" {
			t.Fatal("registered without a client")
		}
	}
}

func TestBurrowLookupNotGated(t *testing.T) {
	if safety.Gated("burrow_lookup") {
		t.Fatal("burrow_lookup is read-only and must not be gated")
	}
}

func checkBurrowCapped(t *testing.T, out string) {
	t.Helper()
	if len(out) > maxToolOutput {
		t.Errorf("output %d bytes, want <= %d", len(out), maxToolOutput)
	}
	if !utf8.ValidString(out) {
		t.Error("output is not valid UTF-8")
	}
	for _, want := range []string{"found: yes", "key: ", "parameter order: "} {
		if !strings.Contains(out, want) {
			t.Errorf("lost %q", want)
		}
	}
}

func TestBurrowLookupToolHugeContract(t *testing.T) {
	var ins []string
	for i := 0; i < 800; i++ {
		ins = append(ins, fmt.Sprintf(`{"name":"param%04d","type":"string"}`, i))
	}
	body, _ := json.Marshal(map[string]any{
		"outcome": "exact", "key": "k@1",
		"contract":  json.RawMessage(`{"verb":"V","object":"O","inputs":[` + strings.Join(ins, ",") + `],"outputs":["int64"]}`),
		"rendering": map[string]any{"lang": "go", "version": 1, "body": "package main\n", "verified_at": "x"},
		"eligible":  true,
	})
	if len(body) < 20<<10 {
		t.Fatalf("contract only %d bytes", len(body))
	}
	ct, _ := burrowToolEnv(t, 200, string(body))
	out, err := runBurrow(t, ct, burrowGoodArgs)
	if err != nil {
		t.Fatal(err)
	}
	checkBurrowCapped(t, out)
	if !strings.Contains(out, "param0000") || !strings.Contains(out, "...") {
		t.Errorf("order not clamped visibly: %.300s", out)
	}
}

func TestBurrowLookupToolHugeKey(t *testing.T) {
	b := strings.Replace(burrowHitBody("package main\n", false), "text/words/count@d10b1d2a09aa", strings.Repeat("k", 5000), 1)
	ct, _ := burrowToolEnv(t, 200, b)
	out, _ := runBurrow(t, ct, burrowGoodArgs)
	checkBurrowCapped(t, out)
	if !strings.Contains(out, "(5000 bytes)") {
		t.Error("key clamp must state the true length")
	}
}

func TestBurrowLookupToolMultibyteBody(t *testing.T) {
	ct, _ := burrowToolEnv(t, 200, burrowHitBody(strings.Repeat("\U0001F600", 5000), false))
	out, _ := runBurrow(t, ct, burrowGoodArgs)
	checkBurrowCapped(t, out)
	if !strings.Contains(out, "[body cut:") {
		t.Error("want a cut note")
	}
}

func TestBurrowFromOptions(t *testing.T) {
	if burrowFromOptions(Options{}) != nil || burrowFromOptions(Options{BurrowURL: "http://x"}) != nil || burrowFromOptions(Options{BurrowTokenFile: "/t"}) != nil {
		t.Error("client built with a missing setting")
	}
	if burrowFromOptions(Options{BurrowURL: "http://x", BurrowTokenFile: "/t"}) == nil {
		t.Error("client not built with both settings")
	}
}
