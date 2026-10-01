//go:build !windows

package runner

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A command can leave a process the group kill cannot reach: a child that calls
// setsid is in a new session and process group. The sweep finds descendants by
// ancestry (ps -ax), remembers them while the command runs (a normal exit
// reparents orphans to pid 1, which hides the ancestry), and SIGKILLs each one
// that is still the same process (pid and start time) after the run, then
// verifies none remain.

const survivorWarning = "runner: a process started by the command survived the post-run sweep"

// psPath is the process lister; tests may replace it.
var psPath = "/bin/ps"

type procInfo struct {
	pid, ppid int
	start     string // lstart text: with the pid it identifies one process
}

// listProcs returns every process of the machine. An error or empty list means
// the sweep cannot see anything; callers treat that as "nothing to do".
func listProcs() []procInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, psPath, "-ax", "-o", "pid=,ppid=,lstart=")
	cmd.Env = []string{"LC_ALL=C"}
	var out bytes.Buffer
	cmd.Stdout = &out
	if cmd.Run() != nil {
		return nil
	}
	var ps []procInfo
	for _, line := range strings.Split(out.String(), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		if e1 != nil || e2 != nil {
			continue
		}
		ps = append(ps, procInfo{pid: pid, ppid: ppid, start: strings.Join(f[2:], " ")})
	}
	return ps
}

// sweeper tracks the descendants of one command's leader.
type sweeper struct {
	root int
	mu   sync.Mutex
	seen map[int]string // pid -> start
}

func newSweeper(root int) *sweeper { return &sweeper{root: root, seen: map[int]string{}} }

// snapshot records every current descendant of the leader and returns them.
func (s *sweeper) snapshot() []procInfo {
	all := listProcs()
	kids := map[int][]procInfo{}
	for _, p := range all {
		kids[p.ppid] = append(kids[p.ppid], p)
	}
	var found []procInfo
	queue := []int{s.root}
	visited := map[int]bool{s.root: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range kids[cur] {
			if visited[c.pid] {
				continue
			}
			visited[c.pid] = true
			found = append(found, c)
			queue = append(queue, c.pid)
		}
	}
	s.mu.Lock()
	for _, p := range found {
		s.seen[p.pid] = p.start
	}
	s.mu.Unlock()
	return found
}

// poll snapshots until stop is closed: every 200 ms at first, when daemonizing
// children appear, then once a second. A command that ends before the first
// sample costs no ps call.
func (s *sweeper) poll(stop <-chan struct{}) {
	began := time.Now()
	for {
		d := time.Second
		if time.Since(began) < 5*time.Second {
			d = 200 * time.Millisecond
		}
		select {
		case <-stop:
			return
		case <-time.After(d):
		}
		s.snapshot()
	}
}

// killEscapees SIGKILLs every current descendant while the leader is alive, so
// ancestry is intact; it repeats so that children forked meanwhile are caught.
func (s *sweeper) killEscapees() {
	for i := 0; i < 3; i++ {
		found := s.snapshot()
		if len(found) == 0 {
			return
		}
		for _, p := range found {
			_ = killProc(p.pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// finish kills every remembered process that is still the same process, waits
// briefly, and returns how many remain.
func (s *sweeper) finish() int {
	s.mu.Lock()
	seen := make(map[int]string, len(s.seen))
	for k, v := range s.seen {
		seen[k] = v
	}
	s.mu.Unlock()
	if len(seen) == 0 {
		return 0 // a command that left nothing behind costs no ps call
	}
	alive := func() []int {
		var pids []int
		for _, p := range listProcs() {
			if st, ok := seen[p.pid]; ok && st == p.start && p.pid != s.root {
				pids = append(pids, p.pid)
			}
		}
		return pids
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		pids := alive()
		if len(pids) == 0 {
			return 0
		}
		if time.Now().After(deadline) {
			return len(pids)
		}
		for _, pid := range pids {
			_ = killProc(pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
