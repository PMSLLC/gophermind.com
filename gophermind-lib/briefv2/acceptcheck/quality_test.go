package acceptcheck

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

var qopts = Options{Bins: []string{"venture-server"}, Env: []string{"TEST_DATABASE_URL", "STUDIO_FAKE_NOW"}}

func has(fs []Finding, want Finding) bool {
	for _, f := range fs {
		if f == want {
			return true
		}
	}
	return false
}

// The 22 root tests the planner wrote for the real AI Venture Studio brief.
func TestQualityRealRehearsalCommands(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id   string
		cmd  string
		want []Finding // each must be present; nil means the command is accepted
	}{
		{"C1", `test -z "$(gofmt -l .)" && go vet ./...`, nil},
		{"C2", `grep -E '^\tgithub.com/jackc/pgx/v5|^\tgolang.org/x/crypto' go.mod | wc -l | grep -q '^2$' || (echo "Unexpected deps" && exit 1)`, nil},
		{"C3", `grep -r 'gorm\|entgo\|sqlx' internal/ || echo "No ORM found"`, []Finding{FindMasked}},
		{"C4", `grep -r 'SELECT\|INSERT\|UPDATE\|DELETE' internal/ --include='*.go' | grep -v 'company_id' | head -1 || true`, []Finding{FindMasked}},
		{"C5", `grep 'argon2' internal/auth/crypto.go | grep -E '64|3|4'`, nil},
		{"C6", `grep -r 'password\|token\|email' internal/httpapi/middleware.go | grep -v 'sanitize\|mask\|filter' || true`, []Finding{FindMasked}},
		{"C7", `grep -r 'time.Second.*60\|Timeout.*60' internal/venture/ llm/`, nil},
		{"C8", `grep -r 'offset\|LIMIT.*OFFSET' internal/ || echo "No offset found"`, []Finding{FindMasked}},
		{"C9", `find internal/httpapi -name '*.go' -exec wc -l {} + | awk '$1 > 60 {print $2}'`, []Finding{FindPrintOnly}},
		{"C10", `find internal -name '*_test.go' | head -5`, []Finding{FindPrintOnly}},
		{"A1", `go build ./...`, nil},
		{"A2", `go vet ./...`, nil},
		{"A3", `unset TEST_DATABASE_URL; go test ./...`, nil},
		{"A4", `go test -tags integration ./...`, nil},
		{"A5", `venture-server migrate up && venture-server seed-templates && venture-server migrate up && venture-server seed-templates`, nil},
		{"A6", `venture-server serve & sleep 2; curl -s localhost:8080/healthz`, []Finding{FindCurlUnasserted}},
		{"A7", `curl -s localhost:8080/v1/openapi.json | jq '.paths | length'`, []Finding{FindCurlUnasserted, FindJqUnasserted}},
		{"A8", `venture-server fake-llm & venture-server serve & sleep 3; STUDIO_LLM_BASE_URL=http://localhost:9000 curl -s localhost:8080/v1/auth/signup ... (simulate flow) ... && echo 'E2E OK'`, []Finding{FindPlaceholder, FindSuccessEcho, FindCurlUnasserted}},
		{"A9", `curl -s localhost:8080/v1/ventures/{id}/pnl/variance | jq '.revenue_variance != 0'`, []Finding{FindPlaceholder, FindJqUnasserted, FindCurlUnasserted}},
		{"A10", `curl -s 'localhost:8080/v1/ventures/{id}/ownership?as_of=2023-01-01' | jq '.holders | map(.percentage) | add == 100.0'`, []Finding{FindPlaceholder, FindJqUnasserted}},
		{"A11", `STUDIO_FAKE_NOW=2023-12-05 venture-server reminder-job && curl -s localhost:8080/v1/accountability | jq '.overdue_tasks > 0'`, []Finding{FindJqUnasserted, FindCurlUnasserted}},
		{"A12", `curl -s -o /dev/null -w '%{http_code}' localhost:8080/v1/ventures/{other_company_id}/venture_id`, []Finding{FindPlaceholder, FindCurlUnasserted}},
	}
	if len(cases) != 22 {
		t.Fatalf("%d cases, want 22", len(cases))
	}
	for _, c := range cases {
		got := Quality(c.cmd, qopts)
		if c.want == nil {
			if len(got) != 0 {
				t.Errorf("%s: want accepted, got %v", c.id, got)
			}
			continue
		}
		if len(got) == 0 {
			t.Errorf("%s: want %v, got none", c.id, c.want)
		}
		for _, w := range c.want {
			if !has(got, w) {
				t.Errorf("%s: want finding %s, got %v", c.id, w, got)
			}
		}
	}
}

func TestQualityGoodRewritesAreAccepted(t *testing.T) {
	t.Parallel()
	good := []string{
		`curl -fsS "$GM_ACCEPTANCE_URL/healthz" | grep -qx ok`,
		`curl -fsS "$GM_ACCEPTANCE_URL/v1/openapi.json" | jq -e '.paths | length >= 90'`,
		`test "$(curl -s -o /dev/null -w '%{http_code}' "$GM_ACCEPTANCE_URL/v1/ventures/none/venture_id")" = 404`,
		`[ "$(curl -s -o /dev/null -w '%{http_code}' "$GM_ACCEPTANCE_URL/x")" = 404 ]`,
		"code=$(curl -s -o /dev/null -w '%{http_code}' \"$GM_ACCEPTANCE_URL/x\")\ntest \"$code\" = 404",
		`curl --fail-with-body -s "$GM_ACCEPTANCE_URL/x" > /dev/null`,
		`curl -fsS "$GM_ACCEPTANCE_URL/x" | jq -er '.ok'`,
		`test "$(curl -fsS "$GM_ACCEPTANCE_URL/x" | jq -r .name)" = Bob`,
		`go build ./... && go vet ./...`,
		`go test ./acceptance -run '^TestA8$' -count=1 -v`,
		`go test -tags integration ./...`,
		`unset TEST_DATABASE_URL; go test ./...`,
		`venture-server migrate up && venture-server seed-templates`,
		`! grep -rq 'gorm' internal/`,
		`test -z "$(grep -rl 'gorm' internal/)"`,
		`STUDIO_FAKE_NOW=2023-12-05 venture-server reminder-job`,
		`grep -rq argon2 internal/auth && go vet ./...`,
		`go vet ./... || exit 1`,
		`kill -0 $$ 2>/dev/null || [ 1 -ge 300 ]`,
		`test -f go.mod || { echo missing; exit 1; }`,
		"venture-server serve &\npid=$!\nsleep 1\ncurl -fsS \"$GM_ACCEPTANCE_URL/healthz\" | grep -qx ok\nrc=$?\nkill $pid\nexit $rc",
		`sh -c 'go vet ./...'`,
		`diff <(echo a) <(echo a)`,
		`awk 'END { exit (NR > 3) ? 0 : 1 }' go.mod`,
		`test "$TEST_DATABASE_URL" = x && go vet ./...`,
		`STUDIO_FAKE_NOW=${STUDIO_FAKE_NOW:-2023-12-05} venture-server reminder-job`,
		`find internal -name '*.go' -exec gofmt -l {} +  | grep -c . | grep -qx 0`,
	}
	for _, cmd := range good {
		if got := Quality(cmd, qopts); len(got) != 0 {
			t.Errorf("%q: got %v", cmd, got)
		}
	}
}

func TestQualityMaskingConstructs(t *testing.T) {
	t.Parallel()
	bad := map[string]string{
		"or true":         `go vet ./... || true`,
		"or colon":        `go vet ./... || :`,
		"or echo":         `go vet ./... || echo skipped`,
		"or exit 0":       `go vet ./... || exit 0`,
		"semicolon true":  `go vet ./...; true`,
		"semicolon colon": "go vet ./...\n:",
		"stderr or":       `go vet ./... 2>/dev/null || echo bad`,
		"set plus e":      `set +e; go vet ./...`,
		"echo after semi": `go vet ./...; echo done`,
		"in sh -c":        `sh -c 'go vet ./... || true'`,
		"group echo only": `go vet ./... || { echo bad; }`,
	}
	for name, cmd := range bad {
		if got := Quality(cmd, qopts); !has(got, FindMasked) {
			t.Errorf("%s: %q got %v", name, cmd, got)
		}
	}
}

func TestQualityPlaceholders(t *testing.T) {
	t.Parallel()
	bad := map[string]string{
		"braces id":    `curl -fsS "$GM_ACCEPTANCE_URL/v/{id}/x" | grep -q ok`,
		"braces dots":  `curl -fsS "$GM_ACCEPTANCE_URL/v" -d '{...}' | grep -q ok`,
		"angle id":     `curl -fsS "$GM_ACCEPTANCE_URL/v/<id>/x" | grep -q ok`,
		"dots arg":     `venture-server migrate ... `,
		"todo":         `go vet ./... # TODO more`,
		"simulate":     `go vet ./... && (simulate the flow)`,
		"dollar var":   `curl -fsS "$GM_ACCEPTANCE_URL/v/$ID" | grep -q ok`,
		"dollar brace": `curl -fsS "$GM_ACCEPTANCE_URL/v/${VENTURE_ID}" | grep -q ok`,
	}
	for name, cmd := range bad {
		got := Quality(cmd, qopts)
		if !has(got, FindPlaceholder) && !has(got, FindUndefinedVar) {
			t.Errorf("%s: %q got %v", name, cmd, got)
		}
	}
	ok := []string{
		`id=$(venture-server mk) && curl -fsS "$GM_ACCEPTANCE_URL/v/$id" | grep -q ok`,
		`for i in 1 2; do curl -fsS "$GM_ACCEPTANCE_URL/v/$i" | grep -q ok || exit 1; done`,
		`go test ./...`,
		`go vet ./...`,
		`curl -fsS "$GM_ACCEPTANCE_URL/x" | jq -e '. as $x | $x.a == 1'`,
		`awk '$1 > 60 { exit 1 }' go.mod`,
		`curl -fsS "$GM_ACCEPTANCE_URL/x" | grep -q '{"ok":true}'`,
		`test "$HOME" != ""`,
		`test -n "$STUDIO_FAKE_NOW" && go vet ./...`,
		`test -n "$UNDECLARED_SECRET" || exit 1; go vet ./...`,
		`id=1; curl -fsS "$GM_ACCEPTANCE_BIN/../x" | grep -q ok`,
	}
	for _, cmd := range ok {
		if got := Quality(cmd, qopts); len(got) != 0 {
			t.Errorf("%q: got %v", cmd, got)
		}
	}
}

func TestQualityCurlAndJqAndPrintOnly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cmd  string
		want Finding
	}{
		{`curl -s "$GM_ACCEPTANCE_URL/x"`, FindCurlUnasserted},
		{`curl -s "$GM_ACCEPTANCE_URL/x" | head -1`, FindCurlUnasserted},
		{`curl -s "$GM_ACCEPTANCE_URL/x" | jq .a`, FindJqUnasserted},
		{`curl -fsS "$GM_ACCEPTANCE_URL/x" | jq '.a == 1'`, FindJqUnasserted},
		{`echo hello`, FindPrintOnly},
		{`printf ok`, FindPrintOnly},
		{`cat go.mod`, FindPrintOnly},
		{`ls internal`, FindPrintOnly},
		{`find internal -name '*.go'`, FindPrintOnly},
		{`go vet ./... && echo 'ALL OK'`, ""},
		{`cat go.mod && echo 'ALL OK'`, FindSuccessEcho},
		{`curl -s "$GM_ACCEPTANCE_URL/x" && echo PASS`, FindSuccessEcho},
		{`echo "$(curl -s "$GM_ACCEPTANCE_URL/x")"`, FindCurlUnasserted},
		{`venture-server serve &`, FindPrintOnly},
		{"venture-server serve &\nsleep 1", FindPrintOnly},
	}
	for _, c := range cases {
		got := Quality(c.cmd, qopts)
		if c.want == "" {
			if len(got) != 0 {
				t.Errorf("%q: got %v", c.cmd, got)
			}
			continue
		}
		if !has(got, c.want) {
			t.Errorf("%q: want %s, got %v", c.cmd, c.want, got)
		}
	}
	if got := Quality(`find internal -name '*.go' -exec test -s {} \;`, qopts); has(got, FindPrintOnly) {
		t.Errorf("find -exec counted as print only: %v", got)
	}
}

func TestQualityFindingsAreFixedAndSortedAndQuoteNothing(t *testing.T) {
	t.Parallel()
	vocab := map[Finding]bool{FindMasked: true, FindSuccessEcho: true, FindPlaceholder: true, FindUndefinedVar: true,
		FindCurlUnasserted: true, FindJqUnasserted: true, FindPrintOnly: true}
	got := Quality(`curl -s "$GM_ACCEPTANCE_URL/SECRETWORD/{id}" | jq .a || true`, qopts)
	if len(got) < 3 {
		t.Fatalf("got %v", got)
	}
	names := FindingNames(got)
	if !sort.StringsAreSorted(names) {
		t.Errorf("not sorted: %v", names)
	}
	for _, f := range got {
		if !vocab[f] {
			t.Errorf("finding %q is outside the vocabulary", f)
		}
		if strings.Contains(string(f), "SECRETWORD") {
			t.Error("a finding quotes the command")
		}
	}
	if !reflect.DeepEqual(names, FindingNames(Quality(`curl -s "$GM_ACCEPTANCE_URL/SECRETWORD/{id}" | jq .a || true`, qopts))) {
		t.Error("not deterministic")
	}
}
