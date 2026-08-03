package mcpclient

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// maxLine bounds a single JSON message read from a server. It matches the
// buffer internal/mcp.Serve uses on its side, so a large tools/list cannot be
// truncated by an asymmetry between our two halves of the same protocol.
const maxLine = 8 * 1024 * 1024

// stdioTransport speaks MCP to a child process over its stdin/stdout, one JSON
// message per line — the framing internal/mcp.Serve implements.
type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu     sync.Mutex // serializes request/response over the single pipe pair
	closed bool
}

// newStdioTransport starts the server process described by cfg.
//
// The child inherits the parent environment plus cfg.Env, so servers that read
// PATH or HOME keep working while per-server overrides still apply.
func newStdioTransport(cfg ServerConfig) (*stdioTransport, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	// Stamp the child so that if it is itself a gophermind, it does not load
	// MCP servers of its own and spawn a grandchild without bound.
	cmd.Env = append(cmd.Env, ChildEnvVar+"=1")
	// A server's diagnostics must not be parsed as protocol; send them to our
	// stderr where the user can actually see them.
	cmd.Stderr = os.Stderr
	setProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %q: %w", cfg.Command, err)
	}

	r := bufio.NewReaderSize(stdout, 64*1024)
	return &stdioTransport{cmd: cmd, stdin: stdin, stdout: r}, nil
}

// Send writes one message and reads the reply. Notifications (no id) get no
// reply, so nothing is read for them — reading would block until the next
// unrelated response and desynchronize the stream.
func (t *stdioTransport) Send(ctx context.Context, msg []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, fmt.Errorf("transport closed")
	}

	if _, err := fmt.Fprintf(t.stdin, "%s\n", msg); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	if isNotification(msg) {
		return nil, nil
	}

	// Reads are blocking, so honor ctx by racing the read against it. The
	// goroutine cannot leak: the buffered channel always accepts its one send,
	// and a cancelled call closes the transport, which unblocks the read.
	type result struct {
		line []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := readLine(t.stdout)
		ch <- result{line, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return r.line, nil
	}
}

// readLine reads one newline-delimited message, skipping blank lines and
// enforcing maxLine.
func readLine(r *bufio.Reader) ([]byte, error) {
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			if err == io.EOF && len(line) == 0 {
				return nil, fmt.Errorf("server closed the connection")
			}
			if err != io.EOF {
				return nil, err
			}
		}
		if len(line) > maxLine {
			return nil, fmt.Errorf("message exceeds %d bytes", maxLine)
		}
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			if err == io.EOF {
				return nil, fmt.Errorf("server closed the connection")
			}
			continue
		}
		return []byte(trimmed), nil
	}
}

// Close shuts the server down: closing stdin asks a well-behaved server to
// exit, then the process group is killed so helper processes a launcher
// spawned (npx spawning node, for instance) do not survive as orphans.
func (t *stdioTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	_ = t.stdin.Close()
	killProcessGroup(t.cmd)
	_ = t.cmd.Wait() // reap; without this the child lingers as a zombie
	return nil
}
