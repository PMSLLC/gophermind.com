package serve

import (
	"bytes"
	"io"
	"strings"
)

// sudoFilter is the stderr of a launched run on its way to run.log. The
// server starts briefs through sudo, and a malformed file in /etc/sudoers.d
// makes sudo print the same harmless complaint before every run. The filter
// drops exactly that complaint: a line starting "/etc/sudoers.d/", a line
// starting "Cmnd_Alias", and, right after one of those, the tilde and caret
// marker lines and the indented "/bin/systemctl" continuation lines. Anything
// else passes through unchanged. hidden counts the "/etc/sudoers.d/" lines.
type sudoFilter struct {
	w       io.Writer
	buf     []byte
	inBlock bool
	hidden  int
}

func (f *sudoFilter) Write(p []byte) (int, error) {
	f.buf = append(f.buf, p...)
	for {
		i := bytes.IndexByte(f.buf, '\n')
		if i < 0 {
			break
		}
		line := f.buf[:i+1]
		err := f.line(line)
		f.buf = f.buf[i+1:]
		if err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

// Flush passes on a last line that had no newline.
func (f *sudoFilter) Flush() error {
	if len(f.buf) == 0 {
		return nil
	}
	line := f.buf
	f.buf = nil
	return f.line(line)
}

func (f *sudoFilter) line(b []byte) error {
	if f.drop(strings.TrimRight(string(b), "\r\n")) {
		return nil
	}
	f.inBlock = false
	_, err := f.w.Write(b)
	return err
}

func (f *sudoFilter) drop(s string) bool {
	switch {
	case strings.HasPrefix(s, "/etc/sudoers.d/"):
		f.hidden++
		f.inBlock = true
		return true
	case strings.HasPrefix(s, "Cmnd_Alias"):
		f.inBlock = true
		return true
	case !f.inBlock:
		return false
	case strings.TrimLeft(s, " \t") != "" && strings.Trim(s, " \t~^") == "":
		return true // marker line: tildes and a caret under the bad spot
	case s != "" && (s[0] == ' ' || s[0] == '\t') && strings.HasPrefix(strings.TrimLeft(s, " \t"), "/bin/systemctl"):
		return true
	}
	return false
}
