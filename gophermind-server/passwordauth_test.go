package main

import (
	"bytes"
	"strings"
	"testing"

	"gophermind/gophermind-lib/serve"
)

// TEMPORARY: tests for passwordauth.go; delete with it.

func localEnv(t *testing.T, extra map[string]string) func(string) string {
	t.Helper()
	h, err := serve.HashPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{
		"GOPHERMIND_TOKEN":             "env-token",
		"GOPHERMIND_SESSION_SECRET":    strings.Repeat("s", 32),
		"GOPHERMIND_PUBLIC_URL":        "https://gophermind.example",
		"GOPHERMIND_WEB_USER":          "admin",
		"GOPHERMIND_WEB_PASSWORD_HASH": h,
	}
	for k, v := range extra {
		m[k] = v
	}
	return envMap(m)
}

func TestLocalLoginOnWithoutGoogle(t *testing.T) {
	cfg, err := parseServerConfig(nil, localEnv(t, nil), fixedGetwd("/cwd"))
	if err != nil {
		t.Fatalf("parseServerConfig: %v", err)
	}
	g := cfg.Google
	if g == nil || g.Local == nil || g.Local.User != "admin" || g.ClientID != "" || g.BearerToken != "env-token" || g.PublicURL != "https://gophermind.example" {
		t.Fatalf("Google = %+v", g)
	}
}

func TestLocalLoginAndGoogleBothOn(t *testing.T) {
	m := map[string]string{}
	for _, k := range []string{"GOPHERMIND_WEB_USER", "GOPHERMIND_WEB_PASSWORD_HASH"} {
		m[k] = localEnv(t, nil)(k)
	}
	m["GOPHERMIND_GOOGLE_CLIENT_ID"] = "cid"
	m["GOPHERMIND_GOOGLE_CLIENT_SECRET"] = "csecret"
	cfg, err := parseServerConfig(nil, googleEnv(m), fixedGetwd("/cwd"))
	if err != nil {
		t.Fatalf("parseServerConfig: %v", err)
	}
	if g := cfg.Google; g == nil || g.Local == nil || g.ClientID != "cid" {
		t.Fatalf("Google = %+v", g)
	}
}

func TestLocalLoginPartialRefuses(t *testing.T) {
	good := localEnv(t, nil)
	for _, drop := range []string{"GOPHERMIND_WEB_USER", "GOPHERMIND_WEB_PASSWORD_HASH", "GOPHERMIND_SESSION_SECRET", "GOPHERMIND_PUBLIC_URL"} {
		_, err := parseServerConfig(nil, localEnv(t, map[string]string{drop: ""}), fixedGetwd("/cwd"))
		if err == nil || !strings.Contains(err.Error(), drop) {
			t.Errorf("without %s: err = %v, want it to name the variable", drop, err)
		}
		if err != nil {
			for _, v := range []string{good("GOPHERMIND_WEB_PASSWORD_HASH"), "env-token", strings.Repeat("s", 32)} {
				if strings.Contains(err.Error(), v) {
					t.Errorf("without %s: error leaks a value: %v", drop, err)
				}
			}
		}
	}
	for name, extra := range map[string]map[string]string{
		"short secret":   {"GOPHERMIND_SESSION_SECRET": "short"},
		"malformed hash": {"GOPHERMIND_WEB_PASSWORD_HASH": "plaintext-password"},
	} {
		_, err := parseServerConfig(nil, localEnv(t, extra), fixedGetwd("/cwd"))
		if err == nil || strings.Contains(err.Error(), "plaintext-password") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestHashpwRoundTrip(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := runHashpw(strings.NewReader("hunter2 with spaces\n"), &out, &errOut); err != nil {
		t.Fatalf("runHashpw: %v", err)
	}
	line := out.String()
	if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		t.Fatalf("want exactly one line, got %q", line)
	}
	line = strings.TrimSuffix(line, "\n")
	if !serve.CheckPassword(line, "hunter2 with spaces") || serve.CheckPassword(line, "hunter2 with spaces\n") {
		t.Fatal("hash does not verify the password (trailing newline must be stripped)")
	}
	if strings.Contains(out.String()+errOut.String(), "hunter2") {
		t.Fatal("password echoed")
	}
	if err := serve.ValidatePasswordHash(line); err != nil {
		t.Fatal(err)
	}
}

func TestHashpwRefusesEmpty(t *testing.T) {
	var out bytes.Buffer
	for _, in := range []string{"", "\n", "\r\n"} {
		if err := runHashpw(strings.NewReader(in), &out, &bytes.Buffer{}); err == nil {
			t.Errorf("accepted empty input %q", in)
		}
	}
	if out.Len() != 0 {
		t.Fatalf("printed %q", out.String())
	}
}
