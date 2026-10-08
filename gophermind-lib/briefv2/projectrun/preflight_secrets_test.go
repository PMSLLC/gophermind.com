package projectrun

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"

	"gophermind/gophermind-lib/briefv2/vault"
)

func TestPreflightVaultPassphrase(t *testing.T) {
	t.Run("not needed", func(t *testing.T) {
		r := newRig(t)
		r.environ[vault.PassphraseEnv] = ""
		c := wantOK(t, r.run(), "vault passphrase")
		if c.Detail != "not needed" {
			t.Errorf("detail %q", c.Detail)
		}
		if _, ok := find(r.run(), "vault"); ok {
			t.Error("vault check when none is needed")
		}
	})
	t.Run("empty env var fails naming it", func(t *testing.T) {
		r := newRig(t)
		r.declare("JWT_SECRET")
		r.environ[vault.PassphraseEnv] = ""
		c := wantFail(t, r.run(), "vault passphrase")
		if !strings.Contains(c.Detail, vault.PassphraseEnv) || c.Fix != "export GOPHERMIND_VAULT_PASSPHRASE=<passphrase>" {
			t.Errorf("%+v", c)
		}
	})
	t.Run("wrong passphrase fails", func(t *testing.T) {
		r := newRig(t)
		r.declare("JWT_SECRET")
		r.seed(vault.HarnessScope, map[string]string{"JWT_SECRET": "CANARY-jwt"})
		r.environ[vault.PassphraseEnv] = "not-the-passphrase"
		c := wantFail(t, r.run(), "vault")
		if strings.Contains(c.Detail+c.Fix, "CANARY") {
			t.Errorf("leak: %+v", c)
		}
	})
	t.Run("missing file passes and is not created", func(t *testing.T) {
		r := newRig(t)
		r.declare("JWT_SECRET")
		res := r.run()
		wantOK(t, res, "vault")
		if _, err := os.Stat(r.vaultPath); !os.IsNotExist(err) {
			t.Fatalf("the vault file was created: %v", err)
		}
		wantFail(t, res, "secret JWT_SECRET")
	})
}

func TestPreflightSecretMatrix(t *testing.T) {
	const canary = "CANARY-9f3a-value"
	r := newRig(t)
	r.declare("IN_VAULT", "GEN_ONLY", "BOTH", "NEITHER", "RUN_ONLY")
	r.seed(vault.HarnessScope, map[string]string{"IN_VAULT": canary, "BOTH": canary})
	r.seed(vault.RunScope(rigRunID), map[string]string{"RUN_ONLY": canary})
	r.o.Generate = map[string]string{"GEN_ONLY": "hex32", "BOTH": "hex32"}

	res := r.run()
	for name, src := range map[string]string{"IN_VAULT": "vault", "GEN_ONLY": "generated:hex32", "BOTH": "vault"} {
		c := wantOK(t, res, "secret "+name)
		if !strings.Contains(c.Detail, src) {
			t.Errorf("%s detail %q, want source %q", name, c.Detail, src)
		}
	}
	c := wantFail(t, res, "secret NEITHER")
	if c.Fix != "gophermind brief vault set NEITHER" {
		t.Errorf("fix %q", c.Fix)
	}
	wantFail(t, res, "secret RUN_ONLY") // the run scope is never read before the plan

	r.o.Attended = true
	res = r.run()
	if c := wantOK(t, res, "secret NEITHER"); !strings.Contains(c.Detail, "prompt") {
		t.Errorf("detail %q", c.Detail)
	}
	for _, c := range res.Checks {
		if strings.Contains(c.Detail+c.Fix, canary) {
			t.Errorf("canary in %+v", c)
		}
	}
}

func listener(t *testing.T) (port string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	_, port, _ = net.SplitHostPort(l.Addr().String())
	return port
}

func TestPreflightDatabaseLoopbackAndDistinct(t *testing.T) {
	live := listener(t)
	_, dead, _ := net.SplitHostPort(closedAddr(t))
	setup := func(a, b string) (*rig, PreflightResult) {
		r := newRig(t)
		r.declare("DATABASE_URL", "TEST_DATABASE_URL")
		r.seed(vault.HarnessScope, map[string]string{"DATABASE_URL": a, "TEST_DATABASE_URL": b})
		return r, r.run()
	}
	noLeak := func(t *testing.T, res PreflightResult) {
		for _, c := range res.Checks {
			if strings.Contains(c.Detail+c.Fix, "s3cretpw") || strings.Contains(c.Detail+c.Fix, "dbusr") || strings.Contains(c.Detail+c.Fix, "dbnamex") {
				t.Errorf("userinfo or database name in %+v", c)
			}
		}
	}
	t.Run("distinct loopback passes", func(t *testing.T) {
		_, res := setup("postgres://dbusr:s3cretpw@127.0.0.1:"+live+"/dbnamex", "postgresql://dbusr:s3cretpw@localhost:"+live+"/dbnamex_test")
		if f := Failed(res.Checks); len(f) != 0 {
			t.Fatalf("%+v", f)
		}
		wantOK(t, res, "database DATABASE_URL")
		wantOK(t, res, "database TEST_DATABASE_URL")
		if c := wantOK(t, res, "database note"); c.Detail != "database emptiness is not checked" {
			t.Errorf("note %q", c.Detail)
		}
		noLeak(t, res)
	})
	t.Run("non-loopback fails", func(t *testing.T) {
		_, res := setup("postgres://dbusr:s3cretpw@db.example.com:6543/dbnamex", "postgres://dbusr:s3cretpw@127.0.0.1:"+live+"/other")
		c := wantFail(t, res, "database DATABASE_URL")
		if c.Detail != "non-loopback host db.example.com:6543: the sandbox denies it" || c.Fix != "ssh -N -L 6543:127.0.0.1:5432 mini" {
			t.Errorf("%+v", c)
		}
		noLeak(t, res)
	})
	t.Run("refused port fails", func(t *testing.T) {
		_, res := setup("postgres://dbusr:s3cretpw@127.0.0.1:"+dead+"/dbnamex", "postgres://dbusr:s3cretpw@127.0.0.1:"+live+"/other")
		c := wantFail(t, res, "database DATABASE_URL")
		if !strings.Contains(c.Fix, "ssh -N -L "+dead+":127.0.0.1:5432 mini") {
			t.Errorf("%+v", c)
		}
		wantOK(t, res, "database TEST_DATABASE_URL")
		noLeak(t, res)
	})
	t.Run("same database twice fails", func(t *testing.T) {
		_, res := setup("postgres://dbusr:s3cretpw@127.0.0.1:"+live+"/dbnamex", "postgres://other:pw2@localhost:"+live+"/dbnamex")
		var found bool
		for _, c := range Failed(res.Checks) {
			if c.Detail == "DATABASE_URL and TEST_DATABASE_URL name the same database" {
				found = true
			}
		}
		if !found {
			t.Fatalf("no duplicate failure in %+v", Failed(res.Checks))
		}
		noLeak(t, res)
	})
	t.Run("non-postgres secret is not a database", func(t *testing.T) {
		r := newRig(t)
		r.declare("JWT_SECRET")
		r.seed(vault.HarnessScope, map[string]string{"JWT_SECRET": "mysql://u:p@db.example.com/x"})
		res := r.run()
		for _, n := range names(res) {
			if strings.HasPrefix(n, "database") {
				t.Errorf("unexpected %q", n)
			}
		}
	})
}

func TestPreflightDatabaseTunnelHint(t *testing.T) {
	r := newRig(t)
	r.declare("DATABASE_URL")
	r.seed(vault.HarnessScope, map[string]string{"DATABASE_URL": "postgres://u:p@127.0.0.1:55432/d"})
	r.env.Dial = func(_ context.Context, addr string) error {
		if addr != "127.0.0.1:55432" {
			t.Errorf("dialed %q", addr)
		}
		return errors.New("refused")
	}
	c := wantFail(t, r.run(), "database DATABASE_URL")
	if c.Fix != "ssh -N -L 55432:127.0.0.1:5432 mini" {
		t.Errorf("fix %q", c.Fix)
	}
}
