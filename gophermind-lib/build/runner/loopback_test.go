package runner

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"
)

func loopbackListener(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen: " + err.Error())
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	n, _ := strconv.Atoi(portPart(l.Addr().String()))
	return n
}

func portPart(addr string) string { _, p, _ := net.SplitHostPort(addr); return p }

// The sandbox lists loopback ports; the runner adds the ones the command's own
// environment names (proxy URL, acceptance address) and the Spec's extras.
func TestRunSandboxLoopbackPorts(t *testing.T) {
	skipIfNoSandbox(t)
	named, extra, other := loopbackListener(t), loopbackListener(t), loopbackListener(t)
	p := sandboxProfile(t)
	r := New(Config{Sandbox: &p})
	run := func(spec Spec, port int) Result {
		spec.Dir, spec.Argv = os.TempDir(), []string{"/bin/bash", "-c", "exec 3<>/dev/tcp/127.0.0.1/" + strconv.Itoa(port)}
		return r.Run(context.Background(), spec)
	}
	env := []string{"PATH=/bin:/usr/bin", "HTTP_PROXY=http://node-a@127.0.0.1:" + strconv.Itoa(named), "GM_ACCEPTANCE_ADDR=localhost:" + strconv.Itoa(named)}
	if res := run(Spec{Env: env}, named); res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("a port named in the environment was refused: %+v", res)
	}
	if res := run(Spec{Env: env}, other); res.ExitCode == 0 {
		t.Fatal("an unnamed loopback port was reachable")
	}
	if res := run(Spec{Env: []string{"PATH=/bin:/usr/bin"}, LoopbackPorts: []int{extra}}, extra); res.ExitCode != 0 {
		t.Fatalf("a Spec port was refused: %+v", res)
	}
	if res := run(Spec{Env: []string{"PATH=/bin:/usr/bin"}}, named); res.ExitCode == 0 {
		t.Fatal("no environment and no ports must reach nothing")
	}
	if len(p.LoopbackPorts) != 0 || len(r.cfg.Sandbox.LoopbackPorts) != 0 {
		t.Fatal("the caller's profile was mutated")
	}
}

func TestEnvPorts(t *testing.T) {
	got := envPorts([]string{"A=http://u:p@127.0.0.1:55432/db", "B=localhost:8080", "C=[::1]:9", "D=example.com:22", "E=127.0.0.1:99999", "F=127.0.0.1:0"})
	want := map[int]bool{55432: true, 8080: true, 9: true}
	if len(got) != len(want) {
		t.Fatalf("ports = %v", got)
	}
	for _, n := range got {
		if !want[n] {
			t.Fatalf("ports = %v", got)
		}
	}
}
