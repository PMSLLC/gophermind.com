package planner

import (
	"reflect"
	"testing"
)

func TestGivenMain(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		dir   string
		want  bool
	}{
		{"main.go with func main", map[string]string{"cmd/app/main.go": "package main\n\nfunc main() {}\n"}, "cmd/app", true},
		{"main only in a test file", map[string]string{"cmd/app/main_test.go": "package main\n\nfunc main() {}\n"}, "cmd/app", false},
		{"package main without func main", map[string]string{"cmd/app/main.go": "package main\n\nfunc helper() {}\n"}, "cmd/app", false},
		{"missing directory", map[string]string{}, "cmd/app", false},
		{"file does not parse", map[string]string{"cmd/app/main.go": "package main\n\nfunc main( {\n"}, "cmd/app", false},
		{"main in a subdirectory only", map[string]string{"cmd/app/sub/main.go": "package main\n\nfunc main() {}\n"}, "cmd/app", false},
		{"func main in a non-main package", map[string]string{"cmd/app/x.go": "package other\n\nfunc main() {}\n"}, "cmd/app", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := t.TempDir()
			for rel, body := range c.files {
				writeRepoFile(t, repo, rel, body)
			}
			if got := givenMain(repo, c.dir); got != c.want {
				t.Fatalf("givenMain = %v, want %v", got, c.want)
			}
		})
	}
}

func TestDropGivenMainsKeepsEverythingElse(t *testing.T) {
	repo := t.TempDir()
	writeRepoFile(t, repo, "cmd/app/main.go", "package main\n\nfunc main() {}\n")
	ifs := []fixedIface{
		{Dir: "internal/cli", Name: "Run", Signature: "func Run(args []string) int"},
		{Dir: "cmd/app", Name: "main", Signature: "func main()"},
		{Dir: "cmd/other", Name: "main", Signature: "func main()"},
	}
	want := []fixedIface{ifs[0], ifs[2]}
	if got := dropGivenMains(ifs, repo); !reflect.DeepEqual(got, want) {
		t.Fatalf("dropGivenMains = %v, want %v", got, want)
	}
}
