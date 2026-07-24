package secaudit

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// skipDirs are trees that are dependencies, build output, or VCS metadata:
// findings there are not the user's code to fix.
var skipDirs = map[string]bool{
	"vendor": true, "dist": true, ".git": true, "node_modules": true,
	"testdata": true, ".venv": true, "venv": true, "__pycache__": true,
	// gophermind's own tooling scratch: review diffs, session logs. Not the
	// audited source, and full of code snippets that trip every rule.
	".superpowers": true, ".remember": true, ".gsd": true,
}

// skipExts are non-source artifacts: diffs and patches embed code that trips
// every rule, and lock/sum files are dependency manifests, not code to fix.
var skipExts = map[string]bool{
	".diff": true, ".patch": true, ".sum": true, ".lock": true,
}

// textRule is a language-agnostic line rule.
type textRule struct {
	id          string
	category    string
	severity    Severity
	re          *regexp.Regexp
	message     string
	remediation string
	// ignore, when set and matching, suppresses the hit (to carve out safe
	// forms like sha256 from a broad crypto pattern).
	ignore *regexp.Regexp
}

// textRules are checked against every line of every text file. Patterns are
// intentionally conservative — a rule that fires on prose trains users to
// ignore the report.
var textRules = []textRule{
	{
		id: "hardcoded-secret (CWE-798)", category: "secrets", severity: High,
		re:          regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		message:     "Hardcoded AWS access key id.",
		remediation: "Move the credential to an environment variable or secret manager and rotate the exposed key.",
	},
	{
		id: "hardcoded-secret (CWE-798)", category: "secrets", severity: High,
		re:          regexp.MustCompile(`(?i)(password|passwd|secret|api[_-]?key|token)\s*[:=]\s*["'][^"']{6,}["']`),
		ignore:      regexp.MustCompile(`(?i)(example|placeholder|dummy|changeme|your[_-]|xxxx|<|\$\{|process\.env|os\.getenv|getenv)`),
		message:     "Possible hardcoded credential assigned in source.",
		remediation: "Load secrets from the environment or a secret store, never literal source; rotate if this was real.",
	},
	{
		id: "private-key (CWE-321)", category: "secrets", severity: Critical,
		re:          regexp.MustCompile(`-----BEGIN (RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----`),
		message:     "Private key material committed to the repository.",
		remediation: "Remove the key, rotate it immediately, and keep private keys out of version control.",
	},
	{
		id: "weak-crypto (CWE-327)", category: "crypto", severity: Medium,
		re:          regexp.MustCompile(`(?i)\b(md5|sha1)\b|\b(des|rc4)\b`),
		ignore:      regexp.MustCompile(`(?i)sha1[0-9]|sha256|sha512|description|address|residual`),
		message:     "Use of a weak or broken cryptographic primitive.",
		remediation: "Use SHA-256+ for hashing and AES-GCM (or better) for encryption.",
	},
	{
		id: "tls-verification-disabled (CWE-295)", category: "tls", severity: High,
		re:          regexp.MustCompile(`(?i)rejectUnauthorized\s*:\s*false|verify\s*=\s*False|CURLOPT_SSL_VERIFYPEER.*0|check_hostname\s*=\s*False`),
		message:     "TLS certificate verification is disabled.",
		remediation: "Keep verification on; for private CAs, trust the CA rather than disabling checks.",
	},
	{
		id: "dynamic-eval (CWE-95)", category: "injection", severity: High,
		re:          regexp.MustCompile(`\beval\s*\(|\bexec\s*\(|new Function\s*\(`),
		ignore:      regexp.MustCompile(`(?i)\.eval\(|evaluate|retrieval|medieval`),
		message:     "Dynamic evaluation of code; if any input is attacker-influenced this is code injection.",
		remediation: "Avoid eval/exec on dynamic input; parse data explicitly (e.g. JSON) or use a safe expression evaluator.",
	},
	{
		id: "insecure-temp-file (CWE-377)", category: "filesystem", severity: Low,
		re:          regexp.MustCompile(`(?i)(/tmp/[A-Za-z0-9_.-]+["'\s]|mktemp\s|tmpnam\s*\()`),
		ignore:      regexp.MustCompile(`(?i)mkstemp|TempFile|TempDir|CreateTemp`),
		message:     "Predictable temporary file path; may allow a symlink/race attack.",
		remediation: "Create temp files atomically with a randomized name (mkstemp / os.CreateTemp).",
	},
}

// ScanStatic runs the deterministic scan over root and returns findings sorted
// worst-first. It never fails on a single bad file; unreadable or unparseable
// files are skipped.
func ScanStatic(root string) ([]Finding, error) {
	var findings []Finding

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip, don't abort the whole scan
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if skipExts[filepath.Ext(path)] {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil || isBinary(data) {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		findings = append(findings, scanText(rel, data)...)
		if strings.HasSuffix(path, ".go") {
			findings = append(findings, scanGo(rel, data)...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sortFindings(findings)
	return findings, nil
}

// isBinary reports whether data looks like a binary file (a NUL byte in the
// first chunk), so the scanner does not emit noise from compiled artifacts.
func isBinary(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

// scanText applies the language-agnostic rules line by line.
func scanText(rel string, data []byte) []Finding {
	var out []Finding
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		for _, r := range textRules {
			if !r.re.MatchString(text) {
				continue
			}
			if r.ignore != nil && r.ignore.MatchString(text) {
				continue
			}
			out = append(out, Finding{
				RuleID: r.id, Severity: r.severity, Category: r.category,
				File: rel, Line: line, Snippet: trimSnippet(text),
				Message: r.message, Remediation: r.remediation,
			})
		}
	}
	return out
}

func trimSnippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

// scanGo runs the AST rules. A parse failure returns no findings rather than an
// error: a half-written file mid-edit must not abort the audit.
func scanGo(rel string, data []byte) []Finding {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, data, 0)
	if err != nil {
		return nil
	}
	var out []Finding
	add := func(pos token.Pos, id, cat string, sev Severity, msg, rem, snip string) {
		out = append(out, Finding{
			RuleID: id, Severity: sev, Category: cat, File: rel,
			Line: fset.Position(pos).Line, Snippet: snip, Message: msg, Remediation: rem,
		})
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.KeyValueExpr:
			// InsecureSkipVerify: true
			if k, ok := node.Key.(*ast.Ident); ok && k.Name == "InsecureSkipVerify" {
				if v, ok := node.Value.(*ast.Ident); ok && v.Name == "true" {
					add(node.Pos(), "tls-verification-disabled (CWE-295)", "tls", High,
						"TLS certificate verification is disabled (InsecureSkipVerify: true).",
						"Remove InsecureSkipVerify; trust a private CA via RootCAs instead of skipping verification.",
						"InsecureSkipVerify: true")
				}
			}
		case *ast.SelectorExpr:
			// math/rand used where crypto randomness is expected.
			if x, ok := node.X.(*ast.Ident); ok && x.Name == "rand" {
				if node.Sel.Name == "Int" || node.Sel.Name == "Intn" || node.Sel.Name == "Read" || node.Sel.Name == "Int63" {
					add(node.Pos(), "weak-random (CWE-338)", "crypto", Medium,
						"math/rand is not cryptographically secure; unsafe for tokens, keys, or nonces.",
						"Use crypto/rand for any security-sensitive randomness.",
						"rand."+node.Sel.Name)
				}
			}
		case *ast.CallExpr:
			// exec.Command with a concatenated argument (shape of command injection).
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "exec" &&
					(sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") {
					for _, arg := range node.Args {
						if isStringConcat(arg) {
							add(node.Pos(), "command-injection (CWE-78)", "injection", High,
								"exec.Command argument is built by string concatenation; if any part is user input this is command injection.",
								"Pass arguments as separate slice elements, never a concatenated string, and validate/allow-list them.",
								"exec."+sel.Sel.Name+"(…+…)")
							break
						}
					}
				}
			}
		}
		return true
	})
	return out
}

// isStringConcat reports whether expr is a "+" binary expression (a heuristic
// for a dynamically built string).
func isStringConcat(expr ast.Expr) bool {
	b, ok := expr.(*ast.BinaryExpr)
	return ok && b.Op == token.ADD
}
