//go:build !windows

package mcpclient

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the child in its own process group so the whole tree can
// be signalled at once. An MCP server is often launched through a wrapper (npx,
// uvx), and killing only the direct child would leave the real server running.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup kills the child's entire process group. The negative pid is
// what makes the signal reach the group rather than just the leader.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		// The group may already be gone, or setpgid may not have taken effect;
		// fall back to the direct child so we never leave it running.
		_ = cmd.Process.Kill()
	}
}
