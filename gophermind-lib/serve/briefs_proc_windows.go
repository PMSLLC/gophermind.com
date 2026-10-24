//go:build windows

package serve

import (
	"errors"
	"os"
	"os/exec"
)

func ownProcessGroup(cmd *exec.Cmd) {}

// signalGroup on Windows can only kill the process itself.
func signalGroup(pgid int, force bool) error {
	p, err := os.FindProcess(pgid)
	if err != nil {
		return err
	}
	if !force {
		return errors.New("graceful stop is not supported on windows")
	}
	return p.Kill()
}
