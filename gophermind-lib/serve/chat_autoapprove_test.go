package serve

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func chatBody(t *testing.T) string {
	t.Helper()
	a := newTestAuth(newFakeGoogle(t, goodClaims()))
	h := pagesMux(t, a)
	c := a.sessionCookie("john@brahy.com", time.Now())
	return do(h, "GET", "/app/chat", nil, c).Body.String()
}

const autoBegin, autoEnd = "/*GM_AUTO_BEGIN*/", "/*GM_AUTO_END*/"

func autoBlock(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, autoBegin)
	j := strings.Index(body, autoEnd)
	if i < 0 || j < i {
		t.Fatal("chat page lacks the GM_AUTO markers")
	}
	return body[i+len(autoBegin) : j]
}

func TestChatAutoApprovePage(t *testing.T) {
	body := chatBody(t)
	for _, want := range []string{
		"Auto approve", "shouldAutoApprove", "Auto approved: ",
		"Auto approve is ON for this chat. Brief actions still ask.",
		"'brief_create'", "'brief_answer'", "'brief_stop'", "'brief_resubmit'",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("chat page lacks %q", want)
		}
	}
	if blk := autoBlock(t, body); strings.Count(blk, "brief_create") != 1 {
		t.Error("brief action names must live in one constant")
	}
	for _, bad := range []string{"localStorage", "sessionStorage", "document.cookie"} {
		if strings.Contains(autoBlock(t, body), bad) {
			t.Errorf("auto approve block uses %s", bad)
		}
		for _, line := range strings.Split(body, "\n") {
			if strings.Contains(line, bad) && (strings.Contains(line, "autoOn") || strings.Contains(line, "autoApprove")) {
				t.Errorf("auto approve persisted via %s: %s", bad, line)
			}
		}
	}
}

func TestChatShouldAutoApproveLogic(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH; skipping shouldAutoApprove logic test")
	}
	blk := autoBlock(t, chatBody(t))
	js := blk + `
const assert = require('assert');
const briefs = ['brief_create','brief_answer','brief_stop','brief_resubmit'];
assert.strictEqual(shouldAutoApprove(false, 'run_shell', false), false);
assert.strictEqual(shouldAutoApprove(false, 'write_file', false), false);
assert.strictEqual(shouldAutoApprove(true, 'run_shell', false), true);
assert.strictEqual(shouldAutoApprove(true, 'write_file', false), true);
for (const b of briefs) assert.strictEqual(shouldAutoApprove(true, b, false), false, b);
assert.strictEqual(shouldAutoApprove(true, 'run_shell', true), false);
assert.strictEqual(shouldAutoApprove(true, 'brief_state', false), true);
assert.strictEqual(shouldAutoApprove(true, 'BRIEF_CREATE', false), false);
console.log('ok');
`
	f := filepath.Join(t.TempDir(), "t.js")
	if err := os.WriteFile(f, []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, f).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
}
