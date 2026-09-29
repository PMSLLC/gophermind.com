package vault_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gophermind/gophermind-lib/briefv2/vault"
)

var fast = vault.Options{WorkFactor: 10}

func newVault(t *testing.T) (*vault.Vault, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sub", "vault.age")
	v, err := vault.Open(p, "correct horse", fast)
	if err != nil {
		t.Fatal(err)
	}
	return v, p
}

func TestRoundTripAndEncryptedAtRest(t *testing.T) {
	v, p := newVault(t)
	const secret = "canary-9f8e7d"
	if err := v.Set(vault.HarnessScope, "CANARY", secret); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, []byte("CANARY")) {
		t.Fatal("vault file contains plaintext")
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	v2, err := vault.Open(p, "correct horse", fast)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := v2.Get(vault.HarnessScope, "CANARY"); !ok || got != secret {
		t.Fatalf("Get = %q, %v", got, ok)
	}
	if names := v2.Names(vault.HarnessScope); len(names) != 1 || names[0] != "CANARY" {
		t.Errorf("Names = %v", names)
	}
}

func TestWrongPassphraseFailsAndKeepsFile(t *testing.T) {
	v, p := newVault(t)
	if err := v.Set(vault.HarnessScope, "KEY_ONE", "value-one"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p)
	_, err := vault.Open(p, "wrong", fast)
	if err == nil {
		t.Fatal("wrong passphrase must fail")
	}
	if strings.Contains(err.Error(), "value-one") {
		t.Error("error leaked a secret value")
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Error("failed Open modified the vault file")
	}
}

func TestSetRejectsBadNames(t *testing.T) {
	v, _ := newVault(t)
	for _, name := range []string{"", "lower", "1ABC", "A-B", "../X"} {
		if err := v.Set(vault.HarnessScope, name, "v"); err == nil {
			t.Errorf("name %q should be rejected", name)
		}
	}
	if err := v.Set(vault.HarnessScope, "OK_NAME", ""); err == nil {
		t.Error("empty value should be rejected")
	}
}

func TestEnvIsScopedAndDoesNotTouchProcessEnv(t *testing.T) {
	v, _ := newVault(t)
	run := vault.RunScope("gm-2026-09-29-001")
	_ = v.Set(run, "CRM_API_KEY", "run-secret")
	_ = v.Set(vault.HarnessScope, "GROQ_API_KEY", "harness-secret")
	env, err := v.Env(run, []string{"CRM_API_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 1 || env[0] != "CRM_API_KEY=run-secret" {
		t.Fatalf("env = %v", env)
	}
	if os.Getenv("CRM_API_KEY") != "" {
		t.Error("Env leaked into the process environment")
	}
	if _, err := v.Env(run, []string{"GROQ_API_KEY"}); err == nil || strings.Contains(err.Error(), "harness-secret") {
		t.Errorf("harness secrets must not resolve in run scope, and errors must not include values: %v", err)
	}
}

func TestConcurrentSetsLoseNothing(t *testing.T) {
	v, p := newVault(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := v.Set(vault.HarnessScope, fmt.Sprintf("KEY_%02d", i), "v"); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	v2, err := vault.Open(p, "correct horse", fast)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(v2.Names(vault.HarnessScope)); n != 20 {
		t.Fatalf("persisted %d names, want 20", n)
	}
}

func TestPassphraseFromEnvAndPipe(t *testing.T) {
	t.Setenv(vault.PassphraseEnv, "from-env")
	got, err := vault.Passphrase("pw: ", os.Stdin, &bytes.Buffer{})
	if err != nil || got != "from-env" {
		t.Fatalf("Passphrase = %q, %v", got, err)
	}
	r, w, _ := os.Pipe()
	_, _ = w.WriteString("piped-value\n")
	_ = w.Close()
	val, err := vault.ReadSecret("v: ", r, &bytes.Buffer{})
	if err != nil || val != "piped-value" {
		t.Fatalf("ReadSecret = %q, %v", val, err)
	}
}
