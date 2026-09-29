package execenv_test

import (
	"reflect"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/brief"
	"gophermind/gophermind-lib/briefv2/execenv"
)

func strp(s string) *string { return &s }

func secrets(vals map[string]string) func([]string) ([]string, error) {
	return func(names []string) ([]string, error) {
		var out []string
		for _, n := range names {
			out = append(out, n+"="+vals[n])
		}
		return out, nil
	}
}

func base() execenv.Inputs {
	return execenv.Inputs{
		Env: []brief.EnvVar{
			{Name: "LISTEN_ADDR", Purpose: "p", Default: strp(":8080")},
			{Name: "LOG_LEVEL", Purpose: "p"},
		},
		Secrets:      []string{"JWT_SIGNING_KEY"},
		SecretValues: secrets(map[string]string{"JWT_SIGNING_KEY": "s3cret-value"}),
		ProxyURL:     "http://node-fn-a@127.0.0.1:8480",
		NodeID:       "fn-a",
	}
}

func TestBuildExactEnvironment(t *testing.T) {
	t.Setenv("HARNESS_ONLY", "leak")
	t.Setenv("GOPHERMIND_VAULT_PASSPHRASE", "leak-pass")
	got, err := execenv.Build(base())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GOPHERMIND_NODE=fn-a",
		"HTTPS_PROXY=http://node-fn-a@127.0.0.1:8480",
		"HTTP_PROXY=http://node-fn-a@127.0.0.1:8480",
		"JWT_SIGNING_KEY=s3cret-value",
		"LISTEN_ADDR=:8080",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	for _, kv := range got {
		if strings.Contains(kv, "leak") {
			t.Errorf("process environment leaked: %q", kv)
		}
	}
}

func TestOverrideBeatsDefaultAndFillsMissingDefault(t *testing.T) {
	in := base()
	in.Overrides = map[string]string{"LISTEN_ADDR": ":9090", "LOG_LEVEL": "debug"}
	got, err := execenv.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "LISTEN_ADDR=:9090") || !strings.Contains(joined, "LOG_LEVEL=debug") || strings.Contains(joined, ":8080") {
		t.Errorf("got %q", got)
	}
}

func TestEnvWithoutDefaultOrOverrideIsOmitted(t *testing.T) {
	got, _ := execenv.Build(base())
	for _, kv := range got {
		if strings.HasPrefix(kv, "LOG_LEVEL=") {
			t.Errorf("LOG_LEVEL has no default and must be omitted, got %q", kv)
		}
	}
}

func TestEmptyDefaultIsSetNotOmitted(t *testing.T) {
	in := base()
	in.Env = []brief.EnvVar{{Name: "EMPTY_OK", Purpose: "p", Default: strp("")}}
	got, err := execenv.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(got, "EMPTY_OK=") {
		t.Errorf("an explicit empty default must be set, got %q", got)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestNoProxyVarsWhenProxyURLEmpty(t *testing.T) {
	in := base()
	in.ProxyURL = ""
	got, _ := execenv.Build(in)
	for _, kv := range got {
		if strings.Contains(kv, "PROXY") {
			t.Errorf("proxy var present without a ProxyURL: %q", kv)
		}
	}
	if !contains(got, "GOPHERMIND_NODE=fn-a") {
		t.Error("GOPHERMIND_NODE must always be set")
	}
}

func TestRejections(t *testing.T) {
	cases := map[string]struct {
		mut  func(*execenv.Inputs)
		want string
	}{
		"override for undeclared name": {func(in *execenv.Inputs) { in.Overrides = map[string]string{"NOPE": "x"} }, "NOPE"},
		"override for a secret name":   {func(in *execenv.Inputs) { in.Overrides = map[string]string{"JWT_SIGNING_KEY": "x"} }, "JWT_SIGNING_KEY"},
		"env name equals secret name": {func(in *execenv.Inputs) {
			in.Env = append(in.Env, brief.EnvVar{Name: "JWT_SIGNING_KEY", Purpose: "p", Default: strp("d")})
		}, "ENV_SECRET_OVERLAP: JWT_SIGNING_KEY"},
		"duplicate env name": {func(in *execenv.Inputs) {
			in.Env = append(in.Env, brief.EnvVar{Name: "LISTEN_ADDR", Purpose: "p"})
		}, "LISTEN_ADDR"},
		"bad node id":              {func(in *execenv.Inputs) { in.NodeID = "../x" }, "node id"},
		"secrets without a source": {func(in *execenv.Inputs) { in.SecretValues = nil }, "SecretValues"},
		"source returns an extra entry": {func(in *execenv.Inputs) {
			in.SecretValues = func([]string) ([]string, error) { return []string{"JWT_SIGNING_KEY=a", "OTHER=b"}, nil }
		}, "secret source"},
		"source returns a malformed entry": {func(in *execenv.Inputs) {
			in.SecretValues = func([]string) ([]string, error) { return []string{"nonsense"}, nil }
		}, "secret source"},
		"source returns too few entries": {func(in *execenv.Inputs) {
			in.SecretValues = func([]string) ([]string, error) { return nil, nil }
		}, "secret source"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			in := base()
			c.mut(&in)
			_, err := execenv.Build(in)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %q, got %v", c.want, err)
			}
			if strings.Contains(err.Error(), "s3cret-value") {
				t.Error("error leaked a secret value")
			}
		})
	}
}

func TestSourceErrorPassesThrough(t *testing.T) {
	in := base()
	in.SecretValues = func([]string) ([]string, error) { return nil, errFake }
	if _, err := execenv.Build(in); err != errFake {
		t.Fatalf("got %v", err)
	}
}

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

var errFake error = fakeErr("vault: secret X is not set in scope run/gm")
