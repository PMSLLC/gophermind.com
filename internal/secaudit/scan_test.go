package secaudit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tree writes a set of files and returns the root.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// ruleIDs collects the rule ids present in a set of findings.
func ruleIDs(fs []Finding) map[string]Finding {
	m := map[string]Finding{}
	for _, f := range fs {
		m[f.RuleID] = f
	}
	return m
}

func hasRulePrefix(fs []Finding, prefix string) bool {
	for _, f := range fs {
		if strings.HasPrefix(f.RuleID, prefix) {
			return true
		}
	}
	return false
}

// --- language-agnostic rules: positive + negative per rule ---

func TestScanFlagsHardcodedAWSKey(t *testing.T) {
	root := tree(t, map[string]string{
		"config.py": "AWS_KEY = \"AKIAIOSFODNN7EXAMPLE\"\n",
	})
	fs, err := ScanStatic(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRulePrefix(fs, "hardcoded-secret") {
		t.Errorf("AWS key not flagged; got %+v", fs)
	}
}

func TestScanDoesNotFlagPlainProse(t *testing.T) {
	root := tree(t, map[string]string{
		"README.md": "This project talks to AWS and uses a password manager.\n",
	})
	fs, err := ScanStatic(root)
	if err != nil {
		t.Fatal(err)
	}
	if hasRulePrefix(fs, "hardcoded-secret") {
		t.Errorf("prose false-positived as a secret: %+v", fs)
	}
}

func TestScanFlagsPrivateKeyBlock(t *testing.T) {
	root := tree(t, map[string]string{
		"id_rsa": "-----BEGIN RSA PRIVATE KEY-----\nMIIabc\n-----END RSA PRIVATE KEY-----\n",
	})
	fs, _ := ScanStatic(root)
	if !hasRulePrefix(fs, "private-key") {
		t.Errorf("private key block not flagged: %+v", fs)
	}
}

func TestScanFlagsWeakCrypto(t *testing.T) {
	root := tree(t, map[string]string{
		"hash.py": "import hashlib\nh = hashlib.md5(data).hexdigest()\n",
	})
	fs, _ := ScanStatic(root)
	if !hasRulePrefix(fs, "weak-crypto") {
		t.Errorf("md5 not flagged: %+v", fs)
	}
}

func TestScanDoesNotFlagSha256(t *testing.T) {
	root := tree(t, map[string]string{
		"hash.py": "import hashlib\nh = hashlib.sha256(data).hexdigest()\n",
	})
	fs, _ := ScanStatic(root)
	if hasRulePrefix(fs, "weak-crypto") {
		t.Errorf("sha256 wrongly flagged as weak: %+v", fs)
	}
}

func TestScanFlagsDisabledTLSAgnostic(t *testing.T) {
	// A non-Go file, so this exercises the text rule, not the AST rule.
	root := tree(t, map[string]string{
		"client.js": "const agent = new https.Agent({ rejectUnauthorized: false });\n",
	})
	fs, _ := ScanStatic(root)
	if !hasRulePrefix(fs, "tls-verification-disabled") {
		t.Errorf("disabled TLS verification not flagged: %+v", fs)
	}
}

func TestScanFlagsDynamicEval(t *testing.T) {
	root := tree(t, map[string]string{
		"run.py": "user = input()\neval(user)\n",
	})
	fs, _ := ScanStatic(root)
	if !hasRulePrefix(fs, "dynamic-eval") {
		t.Errorf("eval of dynamic input not flagged: %+v", fs)
	}
}

// --- skips ---

func TestScanSkipsVendorAndBinaries(t *testing.T) {
	root := tree(t, map[string]string{
		"vendor/lib/x.py":     "password = \"hunter2secretvalue\"\n",
		"dist/build.js":       "eval(userInput)\n",
		"node_modules/a/b.js": "eval(userInput)\n",
	})
	fs, _ := ScanStatic(root)
	if len(fs) != 0 {
		t.Errorf("findings from excluded trees: %+v", fs)
	}
}

func TestScanSkipsOwnBinaryFile(t *testing.T) {
	root := tree(t, map[string]string{
		"blob.bin": string([]byte{0x00, 0x01, 0x02, 'A', 'K', 'I', 'A', 0x00}),
	})
	fs, _ := ScanStatic(root)
	if len(fs) != 0 {
		t.Errorf("binary file scanned: %+v", fs)
	}
}

// --- Go AST rules ---

func TestScanFlagsInsecureSkipVerify(t *testing.T) {
	root := tree(t, map[string]string{
		"tls.go": "package main\nimport \"crypto/tls\"\nvar c = &tls.Config{InsecureSkipVerify: true}\n",
	})
	fs, _ := ScanStatic(root)
	if !hasRulePrefix(fs, "tls-verification-disabled") {
		t.Errorf("InsecureSkipVerify not flagged by AST rule: %+v", fs)
	}
}

func TestScanDoesNotFlagInsecureSkipVerifyFalse(t *testing.T) {
	root := tree(t, map[string]string{
		"tls.go": "package main\nimport \"crypto/tls\"\nvar c = &tls.Config{InsecureSkipVerify: false}\n",
	})
	fs, _ := ScanStatic(root)
	for _, f := range fs {
		if strings.HasPrefix(f.RuleID, "tls-verification-disabled") {
			t.Errorf("InsecureSkipVerify:false wrongly flagged: %+v", f)
		}
	}
}

func TestScanFlagsMathRandForSecret(t *testing.T) {
	root := tree(t, map[string]string{
		"token.go": "package main\nimport \"math/rand\"\nfunc token() int { return rand.Int() }\n",
	})
	fs, _ := ScanStatic(root)
	if !hasRulePrefix(fs, "weak-random") {
		t.Errorf("math/rand not flagged: %+v", fs)
	}
}

func TestScanUnparseableGoFileDoesNotCrash(t *testing.T) {
	root := tree(t, map[string]string{
		"broken.go": "package main\nfunc (\n", // syntactically invalid
	})
	if _, err := ScanStatic(root); err != nil {
		t.Errorf("scan errored on an unparseable Go file: %v", err)
	}
}

// --- findings carry usable metadata ---

func TestFindingsCarryLocationAndRemediation(t *testing.T) {
	root := tree(t, map[string]string{
		"config.py": "\nAWS_KEY = \"AKIAIOSFODNN7EXAMPLE\"\n",
	})
	fs, _ := ScanStatic(root)
	byID := ruleIDs(fs)
	var f Finding
	for id, cand := range byID {
		if strings.HasPrefix(id, "hardcoded-secret") {
			f = cand
		}
	}
	if f.File != "config.py" {
		t.Errorf("File = %q, want repo-relative config.py", f.File)
	}
	if f.Line != 2 {
		t.Errorf("Line = %d, want 2", f.Line)
	}
	if f.Remediation == "" {
		t.Error("finding has no remediation guidance")
	}
	if !strings.Contains(f.RuleID, "CWE-") {
		t.Errorf("RuleID %q is not CWE-tagged", f.RuleID)
	}
}

func TestScanSkipsToolingDirsAndDiffs(t *testing.T) {
	root := tree(t, map[string]string{
		".superpowers/sdd/x.diff": "+password = \"leakedsecret123\"\n",
		".remember/log.md":        "eval(userInput)\n",
		"change.patch":            "+eval(userInput)\n",
		"go.sum":                  "h1:abc password = \"notreal000000\"\n",
	})
	fs, _ := ScanStatic(root)
	if len(fs) != 0 {
		t.Errorf("tooling/diff/patch/lock files were scanned: %+v", fs)
	}
}
