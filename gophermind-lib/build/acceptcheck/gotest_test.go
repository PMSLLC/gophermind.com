package acceptcheck

import (
	"reflect"
	"testing"
)

func TestParseGoTest(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cmd  string
		want GoTest
	}{
		{`go test ./...`, GoTest{Pkg: "./..."}},
		{`go test ./... -count=1`, GoTest{Pkg: "./..."}},
		{`go test -tags integration ./...`, GoTest{Pkg: "./...", Tags: "integration"}},
		{`go test ./... -tags=integration`, GoTest{Pkg: "./...", Tags: "integration"}},
		{`go test -tags acceptance ./acceptance -run '^TestA8$' -count=1 -v`, GoTest{Pkg: "./acceptance", Tags: "acceptance", Funcs: []string{"TestA8"}}},
		{`go test ./internal/db -run '^(TestA|TestB)$'`, GoTest{Pkg: "./internal/db", Funcs: []string{"TestA", "TestB"}}},
		{`go test -race -run TestX ./pkg`, GoTest{Pkg: "./pkg", Race: true, Funcs: []string{"TestX"}}},
		{`unset TEST_DATABASE_URL; go test ./...`, GoTest{Pkg: "./...", Unset: []string{"TEST_DATABASE_URL"}}},
		{"unset A B\nunset C && go test ./...", GoTest{Pkg: "./...", Unset: []string{"A", "B", "C"}}},
		{`STUDIO_FAKE_NOW=2023-12-05 go test ./...`, GoTest{Pkg: "./...", Env: []string{"STUDIO_FAKE_NOW=2023-12-05"}}},
		{`/usr/bin/go test ./...`, GoTest{Pkg: "./..."}},
		{`go test -short ./...`, GoTest{Pkg: "./...", Short: true}},
	}
	for _, c := range cases {
		got, ok := ParseGoTest(c.cmd)
		if !ok {
			t.Errorf("%q: not recognised", c.cmd)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %+v, want %+v", c.cmd, got, c.want)
		}
	}
	not := []string{
		`go build ./...`,
		`go vet ./...`,
		`go test ./... | tee out`,
		`go test ./... || true`,
		`go build ./... && go test ./...`,
		`go test ./... && echo ok`,
		`go test ./a ./b`,
		`go test`,
		`go test -exec foo ./...`,
		`go test -coverprofile=c.out ./...`,
		`go test ./... &`,
		`sh -c 'go test ./...'`,
		`go test -run 'Test.*Foo' ./...`,
		`go test -json ./...`,
		`echo go test ./...`,
		`go test $(echo ./...)`,
		`go test -tags 'a b' ./...`,
	}
	for _, cmd := range not {
		if got, ok := ParseGoTest(cmd); ok {
			t.Errorf("%q: recognised as %+v", cmd, got)
		}
	}
}

func TestServerExercising(t *testing.T) {
	t.Parallel()
	bins := []string{"venture-server"}
	yes := []string{
		`venture-server migrate up`,
		`venture-server serve & sleep 2; curl -s localhost:8080/healthz`,
		`curl -fsS "$GM_ACCEPTANCE_URL/x"`,
		`test -n "$GM_ACCEPTANCE_ADDR" && nc -z 127.0.0.1 80`,
		`go test -tags acceptance ./acceptance -run '^TestA8$' -count=1 -v`,
		`go test ./acceptance`,
		`"$GM_ACCEPTANCE_BIN"/venture-server serve`,
		`sh -c 'venture-server migrate up'`,
		`test "$(venture-server version)" = v1`,
		`wget -qO- http://localhost/x`,
	}
	for _, c := range yes {
		if !ServerExercising(c, bins) {
			t.Errorf("%q: not server-exercising", c)
		}
	}
	no := []string{
		`go build ./...`,
		`go vet ./...`,
		`go test ./...`,
		`go test -tags integration ./internal/db`,
		`test -z "$(gofmt -l .)" && go vet ./...`,
		`! grep -rq gorm internal/`,
		`grep -r 'SELECT' internal/ | head -1`,
		`unset TEST_DATABASE_URL; go test ./...`,
		`echo curl`,
		`grep -q localhost.txt README.md`,
	}
	for _, c := range no {
		if ServerExercising(c, bins) {
			t.Errorf("%q: counted as server-exercising", c)
		}
	}
}
