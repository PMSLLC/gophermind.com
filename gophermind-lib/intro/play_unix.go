//go:build unix

package intro

import (
	"os"
	"syscall"

	"golang.org/x/term"
)

// Play runs the intro on stdout when the terminal is suitable, returning
// immediately otherwise. Any keypress skips the rest. It puts stdin into raw,
// non-blocking mode so a single keystroke is detected without waiting for Enter,
// then fully restores stdin (blocking + cooked) and drains any leftover input so
// the TUI that follows starts from a clean slate.
func Play() {
	if !shouldPlay() {
		return
	}
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return // can't manage the terminal safely — skip rather than risk it
	}
	_ = syscall.SetNonblock(fd, true)
	defer func() {
		_ = syscall.SetNonblock(fd, false)
		_ = term.Restore(fd, old)
	}()

	out := os.Stdout
	out.WriteString(altScreen + clear + hideCursor)
	defer func() {
		out.WriteString(reset + showCursor + mainScreen)
		drain(fd)
	}()

	playSequence(out, termWidth(), func() bool { return keyPressed(fd) })
}

// keyPressed reports whether any input byte is available on the (non-blocking)
// terminal fd. A read of 0 or an error (EAGAIN when empty) means "no key".
func keyPressed(fd int) bool {
	var buf [16]byte
	n, err := syscall.Read(fd, buf[:])
	return err == nil && n > 0
}

// drain consumes any pending input bytes so a leftover keystroke from the intro
// (including the skip key) does not spill into the next reader.
func drain(fd int) {
	var buf [64]byte
	for {
		n, err := syscall.Read(fd, buf[:])
		if n <= 0 || err != nil {
			return
		}
	}
}
