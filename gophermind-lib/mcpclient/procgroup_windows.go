//go:build windows

package mcpclient

import "os/exec"

// setProcessGroup is a no-op on Windows, which has no setpgid. Grouping would
// need a Job Object; gophermind does not need that today.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup kills the direct child. Any helper processes it spawned are
// left to exit on their own once their stdin closes.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
