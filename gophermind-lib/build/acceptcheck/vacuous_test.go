package acceptcheck

import "testing"

func TestVacuousRefuses(t *testing.T) {
	t.Parallel()
	refused := map[string]string{
		"true":                 "true",
		"colon":                ":",
		"exit 0":               "exit 0",
		"echo":                 `echo "$GM_ACCEPTANCE_URL"`,
		"printf":               `printf ok`,
		"sleep":                `sleep 1`,
		"test alone":           `test -n "$GM_ACCEPTANCE_URL"`,
		"bracket alone":        `[ -n "$GM_ACCEPTANCE_URL" ]`,
		"several no-ops":       "true; echo hi\nexit 0",
		"go run":               `go run ./cmd/greeter & curl -sf "$GM_ACCEPTANCE_URL"`,
		"go install":           `go install ./cmd/greeter && curl -sf "$GM_ACCEPTANCE_URL"`,
		"go get":               `go get example.com/x && curl -sf "$GM_ACCEPTANCE_URL"`,
		"go generate":          `go generate ./... && curl -sf "$GM_ACCEPTANCE_URL"`,
		"rm the bin dir":       `rm -f "$GM_ACCEPTANCE_BIN"/greeter; curl -sf "$GM_ACCEPTANCE_URL"`,
		"cp over a bin":        `cp /bin/ls "$GM_ACCEPTANCE_BIN/greeter"; curl -sf "$GM_ACCEPTANCE_URL"`,
		"comments only":        "# nothing here\n",
		"go build alone":       `go build`,
		"relative path":        `./greeter --addr "$GM_ACCEPTANCE_ADDR" & curl -sf "$GM_ACCEPTANCE_URL"`,
		"absolute path":        `/tmp/x/greeter --addr "$GM_ACCEPTANCE_ADDR" & curl -sf "$GM_ACCEPTANCE_URL"`,
		"no url at all":        `grep -q x README.md`,
		"exit then curl":       `exit 0 && curl x`,
		"true or curl":         `true || curl x`,
		"if false":             "if false; then curl x; fi",
		"curl in a comment":    `ls # go build ./...`,
		"go test in a string":  `ls; echo "go test ./..."`,
		"test of the url":      `ls; test -n "$GM_ACCEPTANCE_URL"`,
		"echo go build":        `echo go build ./...`,
		"true then comment":    `true # curl`,
		"colon then word":      `: curl`,
		"sh -c go run":         `sh -c 'go run ./x'`,
		"bash -c go run":       `bash -c "go run ./x"`,
		"unconditional exit":   "exit 0\ncurl -s localhost:8080",
		"exec then curl":       "exec true; curl x",
		"localhost filename":   `ls localhost.txt`,
		"localhost as a word":  `grep localhost config.yml`,
		"127 prefix only":      `ls 127.0.0.1x`,
		"go build no package":  `go build -o x`,
		"go test flags only":   `go test -v -count=1`,
		"unterminated heredoc": "cat <<EOF\ncurl x\n",
		"heredoc data only":    "cat <<'EOF'\ncurl -s localhost:8080\ngo test ./...\nEOF\nls",
		"false and curl":       `false && curl x`,
		"if true else":         "if true; then ls; else curl x; fi",
		"while false":          "while false; do curl x; done",
	}
	for name, cmd := range refused {
		if !Vacuous(cmd, nil) {
			t.Errorf("%s: %q was accepted", name, cmd)
		}
	}
}

func TestVacuousAccepts(t *testing.T) {
	t.Parallel()
	accepted := map[string]string{
		"curl with the base url":  `curl -sf "$GM_ACCEPTANCE_URL/hello" | grep -q Hello`,
		"binary by name":          `greeter --addr "$GM_ACCEPTANCE_ADDR" & pid=$!; curl -sf "$GM_ACCEPTANCE_URL/hello"; kill $pid`,
		"go build all":            `go build ./...`,
		"go vet all":              `go vet ./...`,
		"go test all":             `go test ./...`,
		"go test tags":            `go test ./... -tags integration`,
		"go test a path":          `go test ./internal/db`,
		"curl elsewhere":          `curl -sf https://example.com/`,
		"go build then curl":      `go build -o /tmp/x ./cmd/greeter; curl -sf "$GM_ACCEPTANCE_URL"`,
		"echo then curl":          "echo start\ncurl -sf \"$GM_ACCEPTANCE_URL/hello\"",
		"system curl by path":     `/usr/bin/curl -sf "$GM_ACCEPTANCE_URL/hello"`,
		"sh -c go test":           `sh -c 'go test ./...'`,
		"bash -c curl":            `bash -c "curl -s localhost:8080/healthz"`,
		"serve and curl":          `venture-server serve & sleep 1; curl -s localhost:8080/healthz | grep -q ok`,
		"multi line script":       "greeter --addr \"$GM_ACCEPTANCE_ADDR\" &\npid=$!\nsleep 1\ncurl -s \"$GM_ACCEPTANCE_URL/hello\" | grep -q Hello\nrc=$?\nkill $pid\nexit $rc",
		"exit guarded":            "test -n x || exit 1\ncurl -s localhost:8080",
		"if curl":                 "if curl -sf localhost:8080; then ls; fi",
		"if true then":            "if true; then curl x; fi",
		"curl in subshell":        `ls "$(curl -s localhost:8080)"`,
		"env wrapper":             `env FOO=1 curl -s localhost:8080`,
		"guard group":             "test -f go.mod || { echo no; exit 1; }\ncurl -s localhost:8080",
		"guard exit":              "[ -x bin ] || exit 1; curl -s localhost:8080",
		"set -e":                  "set -e; curl -s localhost:8080",
		"trap":                    "trap 'kill $pid' EXIT; curl -s localhost:8080",
		"function before use":     "f() { curl -s localhost:8080; }\nf",
		"subshell guard":          "(test -f go.mod || exit 1); curl -s localhost:8080",
		"if guard":                "if [ ! -f go.mod ]; then exit 1; fi\ncurl -s localhost:8080",
		"go build -o dot":         `go build -o x .`,
		"go vet dot":              `go vet .`,
		"go test dot":             `go test .`,
		"go test flags":           `go test -count=1 -run X -race ./internal/db`,
		"go build -o tags":        `go build -o /tmp/x -tags integration ./cmd/greeter`,
		"go test dots":            `go test -race ./pkg/...`,
		"go test import path":     `go test example.com/m/pkg`,
		"url localhost":           `wget -qO- http://localhost/x`,
		"bare loopback ip":        `nc -z 127.0.0.2 80`,
		"ipv6 loopback":           `nc -z ::1 80`,
		"heredoc then curl":       "cat <<EOF >/dev/null\ncurl x\nEOF\ncurl -s localhost:8080",
		"heredoc dash":            "cat <<-EOF >/dev/null\n\tEOF\ncurl -s localhost:8080",
		"known limit variable":    `c=true; $c || curl x`,
		"known limit uncalled fn": `f() { curl x; }`,
		"from the bin dir":        `"$GM_ACCEPTANCE_BIN"/greeter --addr "$GM_ACCEPTANCE_ADDR" & curl -sf "$GM_ACCEPTANCE_URL"`,
	}
	for name, cmd := range accepted {
		if Vacuous(cmd, nil) {
			t.Errorf("%s: %q was refused", name, cmd)
		}
	}
}

func TestVacuousBuiltBinaryNames(t *testing.T) {
	t.Parallel()
	cmd := `venture-server migrate up && venture-server seed-templates`
	if !Vacuous(cmd, nil) {
		t.Error("a bare program was accepted with no built binary named like it")
	}
	if Vacuous(cmd, []string{"venture-server"}) {
		t.Error("the built binary by name was refused")
	}
	if !Vacuous(cmd, []string{"greeter"}) {
		t.Error("a program that is not a built binary was accepted")
	}
}

func TestUnsetNames(t *testing.T) {
	t.Parallel()
	declared := map[string]bool{"GREETER_TOKEN": true}
	if got := UnsetNames("`go vet ./...` reports nothing with `GREETER_TOKEN` unset", declared); len(got) != 1 || got[0] != "GREETER_TOKEN" {
		t.Errorf("got %v", got)
	}
	for _, text := range []string{"with greeter_token unset", "with NOT_DECLARED unset", "GREETER_TOKEN unset", "with GREETER_TOKEN removed"} {
		if got := UnsetNames(text, declared); len(got) != 0 {
			t.Errorf("UnsetNames(%q) = %v", text, got)
		}
	}
}
