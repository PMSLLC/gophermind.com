package tuning

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProfilesAreKnown covers the argument surface of /optimize.
func TestProfilesAreKnown(t *testing.T) {
	for _, name := range []string{"safe", "balanced", "aggressive", "unattended"} {
		if _, ok := Lookup(name); !ok {
			t.Errorf("profile %q not found", name)
		}
	}
	if _, ok := Lookup("nonsense"); ok {
		t.Error("unknown profile should not resolve")
	}
	if len(Names()) < 4 {
		t.Errorf("Names() = %v, want at least the four profiles", Names())
	}
}

// TestAggressiveIsFasterThanSafe pins the actual ordering the names promise.
func TestAggressiveIsFasterThanSafe(t *testing.T) {
	safe := Settings(mustProfile(t, "safe"), Probe{})
	aggr := Settings(mustProfile(t, "aggressive"), Probe{})

	if !gt(aggr["GOPHERMIND_MAX_ITER"], safe["GOPHERMIND_MAX_ITER"]) {
		t.Errorf("aggressive MAX_ITER (%s) should exceed safe (%s)",
			aggr["GOPHERMIND_MAX_ITER"], safe["GOPHERMIND_MAX_ITER"])
	}
	if !gt(aggr["GOPHERMIND_MAX_ATTEMPTS"], safe["GOPHERMIND_MAX_ATTEMPTS"]) {
		t.Error("aggressive should retry more than safe")
	}
	if aggr["GOPHERMIND_CACHE_ENABLED"] != "1" {
		t.Error("aggressive should enable the response cache")
	}
}

// TestSafetyStaysOnUnlessUnattended is the guarantee behind the profile split:
// asking for speed must never remove the approval gate or the guards.
func TestSafetyStaysOnUnlessUnattended(t *testing.T) {
	for _, name := range []string{"safe", "balanced", "aggressive"} {
		s := Settings(mustProfile(t, name), Probe{})
		if s["GOPHERMIND_APPROVAL"] != "ask" {
			t.Errorf("%s: APPROVAL = %q, want ask", name, s["GOPHERMIND_APPROVAL"])
		}
		if s["GOPHERMIND_INSECURE_TLS"] != "0" {
			t.Errorf("%s: INSECURE_TLS = %q, want 0", name, s["GOPHERMIND_INSECURE_TLS"])
		}
	}
	un := Settings(mustProfile(t, "unattended"), Probe{})
	if un["GOPHERMIND_APPROVAL"] != "auto" {
		t.Errorf("unattended: APPROVAL = %q, want auto", un["GOPHERMIND_APPROVAL"])
	}
	// Even unattended must not disable TLS verification.
	if un["GOPHERMIND_INSECURE_TLS"] != "0" {
		t.Errorf("unattended: INSECURE_TLS = %q, want 0", un["GOPHERMIND_INSECURE_TLS"])
	}
}

// TestProbeWidensTimeouts: a slow endpoint must not be given the same timeouts
// as a fast one, or long generations get cut off.
func TestProbeWidensTimeouts(t *testing.T) {
	fast := Settings(mustProfile(t, "aggressive"), Probe{TokensPerSecond: 120, ContextWindow: 32768})
	slow := Settings(mustProfile(t, "aggressive"), Probe{TokensPerSecond: 4, ContextWindow: 32768})

	if !gt(slow["GOPHERMIND_STREAM_IDLE_TIMEOUT_S"], fast["GOPHERMIND_STREAM_IDLE_TIMEOUT_S"]) {
		t.Errorf("slow endpoint idle timeout (%s) should exceed fast (%s)",
			slow["GOPHERMIND_STREAM_IDLE_TIMEOUT_S"], fast["GOPHERMIND_STREAM_IDLE_TIMEOUT_S"])
	}
}

// TestMergeIntoEnvFilePreservesExistingValues is the critical one: the user's
// .env holds their live endpoint and model, which must survive optimization.
func TestMergeIntoEnvFilePreservesExistingValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	existing := "# my notes\nGOPHERMIND_BASE_URL=http://10.0.0.5:1234/v1\nGOPHERMIND_MODEL=my-model\nGOPHERMIND_MAX_ITER=25\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := MergeIntoEnvFile(path, map[string]string{
		"GOPHERMIND_MAX_ITER":      "60",
		"GOPHERMIND_CACHE_ENABLED": "1",
	})
	if err != nil {
		t.Fatalf("MergeIntoEnvFile: %v", err)
	}
	if changed == 0 {
		t.Error("no changes reported")
	}

	got := readEnv(t, path)
	if got["GOPHERMIND_BASE_URL"] != "http://10.0.0.5:1234/v1" {
		t.Errorf("BASE_URL was clobbered: %q", got["GOPHERMIND_BASE_URL"])
	}
	if got["GOPHERMIND_MODEL"] != "my-model" {
		t.Errorf("MODEL was clobbered: %q", got["GOPHERMIND_MODEL"])
	}
	if got["GOPHERMIND_MAX_ITER"] != "60" {
		t.Errorf("MAX_ITER = %q, want the tuned 60", got["GOPHERMIND_MAX_ITER"])
	}
	if got["GOPHERMIND_CACHE_ENABLED"] != "1" {
		t.Errorf("CACHE_ENABLED not added: %q", got["GOPHERMIND_CACHE_ENABLED"])
	}
	// Comments and unrelated content survive.
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "# my notes") {
		t.Error("comment was lost")
	}
}

// TestMergeNeverTouchesSecrets: values the user set for credentials must not be
// overwritten even if a profile has an opinion about the key.
func TestMergeNeverOverwritesNonEmptySecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("GOPHERMIND_API_KEY=sk-live-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := MergeIntoEnvFile(path, map[string]string{"GOPHERMIND_API_KEY": ""}); err != nil {
		t.Fatal(err)
	}
	if got := readEnv(t, path)["GOPHERMIND_API_KEY"]; got != "sk-live-secret" {
		t.Errorf("API key was overwritten: %q", got)
	}
}

// TestMergeCreatesFileWhenAbsent covers a fresh checkout.
func TestMergeCreatesFileWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if _, err := MergeIntoEnvFile(path, map[string]string{"GOPHERMIND_MAX_ITER": "60"}); err != nil {
		t.Fatalf("MergeIntoEnvFile: %v", err)
	}
	if readEnv(t, path)["GOPHERMIND_MAX_ITER"] != "60" {
		t.Error("value not written to the new file")
	}
}

// TestMergePreservesFilePermissions keeps a 0600 .env from being widened.
func TestMergePreservesRestrictivePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("GOPHERMIND_MODEL=m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := MergeIntoEnvFile(path, map[string]string{"GOPHERMIND_MAX_ITER": "60"}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
}

func mustProfile(t *testing.T, name string) Profile {
	t.Helper()
	p, ok := Lookup(name)
	if !ok {
		t.Fatalf("profile %q missing", name)
	}
	return p
}

func gt(a, b string) bool {
	return len(a) > len(b) || (len(a) == len(b) && a > b)
}

func readEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}
