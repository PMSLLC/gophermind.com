package serve

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sudoNoise = `/etc/sudoers.d/gmrun:3:12: syntax error
Cmnd_Alias GMRUN = /bin/systemctl restart a, \
           ^~~~~
    /bin/systemctl restart b
/etc/sudoers.d/gmrun:4:1: syntax error
Cmnd_Alias OTHER = /bin/true
~~~^
`

func TestSudoFilterDropsOnlySudoersNoise(t *testing.T) {
	var out bytes.Buffer
	f := &sudoFilter{w: &out}
	in := sudoNoise + "real error: boom\n    /bin/systemctl is mentioned by the tool\n^ not a marker\n" +
		"/etc/sudoers.d/x:1: warning\n  /bin/systemctl reload\nlast line without newline"
	// Split mid-line to prove buffering.
	f.Write([]byte(in[:40]))
	f.Write([]byte(in[40:]))
	f.Flush()
	want := "real error: boom\n    /bin/systemctl is mentioned by the tool\n^ not a marker\nlast line without newline"
	if out.String() != want {
		t.Errorf("got %q\nwant %q", out.String(), want)
	}
	if f.hidden != 3 {
		t.Errorf("hidden = %d, want 3", f.hidden)
	}
}

func TestBriefRunLogHidesSudoWarningsAndCountsThem(t *testing.T) {
	e := newBriefEnv(t)
	e.script = func(args []string) string {
		return "cat >&2 <<'EOF'\n" + sudoNoise + "EOF\necho real stderr >&2; echo ran"
	}
	id := "gm-2026-10-23-071"
	if w := e.post("/app/briefs", e.briefText(id), nil); w.Code != http.StatusAccepted {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	e.waitPhase(id, phaseDone, phaseFailed)
	e.waitNotActive(id)
	b, err := os.ReadFile(filepath.Join(e.dir, id, "run.log"))
	if err != nil {
		t.Fatal(err)
	}
	log := string(b)
	for _, bad := range []string{"/etc/sudoers.d/", "Cmnd_Alias", "~~~"} {
		if strings.Contains(log, bad) {
			t.Errorf("log still holds %q:\n%s", bad, log)
		}
	}
	if strings.Count(log, "real stderr") != 2 || !strings.Contains(log, "ran") {
		t.Errorf("real output lost:\n%s", log)
	}
	// Two steps, two warnings each: one count line for the whole run.
	if strings.Count(log, "sudo warnings hidden)") != 1 || !strings.Contains(log, "(4 sudo warnings hidden)") {
		t.Errorf("count line wrong:\n%s", log)
	}
}
