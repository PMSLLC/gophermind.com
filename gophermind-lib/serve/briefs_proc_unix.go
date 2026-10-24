//go:build !windows

package serve

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup puts the command in a new process group, so a stop reaches
// the command and everything it started.
func ownProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// signalGroup sends SIGTERM (or SIGKILL when force) to the process group pgid.
func signalGroup(pgid int, force bool) error {
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	return syscall.Kill(-pgid, sig)
}
