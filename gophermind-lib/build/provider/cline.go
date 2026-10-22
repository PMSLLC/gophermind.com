//go:build !windows

package provider

// The Cline provider runs the Cline CLI headlessly instead of making one LLM
// call. Three rules keep it from touching anything it should not. Cline works in
// a scratch copy of the worktree (no .git, no symlinks) and the real worktree is
// only ever read. Cline runs in act mode from an isolated data dir, because its
// default Plan mode refuses to write files; the user's providers.json is linked
// in, never copied. The child gets an environment allowlist, so no API key or
// token from the parent reaches it.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var _ Provider = (*Cline)(nil)

// clineTrailer ends every prompt; %s is the target file, relative to the scratch copy.
const clineTrailer = "\n\nYou are working in a scratch copy of the repository in the current directory. Do not reply with code. Instead write the result to the file %s in the current directory, creating or overwriting only that file. Run the package's tests with go test to check your work. Change no other file. When you are finished reply with one short sentence."

const (
	clineSettings   = `{"autoUpdateEnabled":false,"telemetryOptOut":true,"planActMode":"act"}`
	clineMaxTarget  = 1 << 20
	clineMaxLine    = 4 << 20
	clineMaxCopy    = 64 << 20
	clineMaxFiles   = 5000
	clineTempPrefix = "gm-cline-"
)

// clineEnv are the only variables the child inherits.
var clineEnv = []string{"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "GOPATH", "GOCACHE", "GOMODCACHE", "GOFLAGS", "GOROOT", "GOPROXY", "GONOSUMCHECK", "GONOSUMDB", "GOTOOLCHAIN"}

// ClineConfig configures the Cline provider. Zero values take the defaults noted.
type ClineConfig struct {
	Name          string
	Binary        string        // default "cline"
	ClineProvider string        // the -P value, for example "cline-pass"
	SettingsDir   string        // holds providers.json; default ~/.cline/data/settings
	Timeout       time.Duration // default 15m
	MaxConcurrent int           // informational, like the OpenAI provider
	Models        []ModelInfo
	MaxCopyBytes  int64 // default 64 MiB
	MaxCopyFiles  int   // default 5000
}

// Cline completes a request by letting the Cline CLI write the target file in a
// scratch copy of the worktree and returning that file as a fenced reply.
//
// Cline makes its own network calls, outside the harness proxy, so use it only
// for providers whose visibility is public. It is opt-in.
type Cline struct{ cfg ClineConfig }

func NewCline(cfg ClineConfig) *Cline {
	if cfg.Binary == "" {
		cfg.Binary = "cline"
	}
	if cfg.SettingsDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfg.SettingsDir = filepath.Join(home, ".cline", "data", "settings")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if cfg.SettingsDir == "~" {
			cfg.SettingsDir = home
		} else if strings.HasPrefix(cfg.SettingsDir, "~/") {
			cfg.SettingsDir = filepath.Join(home, cfg.SettingsDir[2:])
		}
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Minute
	}
	if cfg.MaxCopyBytes <= 0 {
		cfg.MaxCopyBytes = clineMaxCopy
	}
	if cfg.MaxCopyFiles <= 0 {
		cfg.MaxCopyFiles = clineMaxFiles
	}
	return &Cline{cfg: cfg}
}

func (c *Cline) Name() string        { return c.cfg.Name }
func (c *Cline) Models() []ModelInfo { return c.cfg.Models }

func (c *Cline) Complete(ctx context.Context, req Request) (Response, error) {
	start := time.Now()
	if err := validateClineRequest(req); err != nil {
		return Response{}, err
	}
	providers := filepath.Join(c.cfg.SettingsDir, "providers.json")
	if fi, err := os.Stat(providers); err != nil || !fi.Mode().IsRegular() {
		return Response{}, ErrAuth{Provider: c.cfg.Name}
	}

	tmp, err := os.MkdirTemp("", clineTempPrefix)
	if err != nil {
		return Response{}, fmt.Errorf("cline: creating the scratch directory: %w", err)
	}
	defer removeClineTemp(tmp)
	work := filepath.Join(tmp, "work")
	settings := filepath.Join(tmp, "data", "settings")
	for _, d := range []string{work, settings} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return Response{}, fmt.Errorf("cline: creating the scratch directory: %w", err)
		}
	}
	if err := copyTree(req.Workdir, work, c.cfg.MaxCopyBytes, c.cfg.MaxCopyFiles); err != nil {
		return Response{}, err
	}
	if err := os.WriteFile(filepath.Join(settings, "global-settings.json"), []byte(clineSettings), 0o600); err != nil {
		return Response{}, fmt.Errorf("cline: writing the settings: %w", err)
	}
	if err := os.Symlink(providers, filepath.Join(settings, "providers.json")); err != nil {
		return Response{}, fmt.Errorf("cline: linking the providers file: %w", err)
	}

	var parts []string
	for _, m := range req.Messages {
		parts = append(parts, m.Content)
	}
	prompt := strings.Join(parts, "\n\n") + fmt.Sprintf(clineTrailer, req.TargetFile)

	runCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	secs := int(c.cfg.Timeout / time.Second)
	if secs < 1 {
		secs = 1
	}
	cmd := exec.CommandContext(runCtx, c.cfg.Binary,
		"--json", "--auto-approve", "true",
		"--data-dir", filepath.Join(tmp, "data"),
		"-P", c.cfg.ClineProvider, "-m", req.Model,
		"-c", work, "-t", strconv.Itoa(secs), prompt)
	cmd.Env = clineChildEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var out, errOut clineLines
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	out.stdout, errOut.stdout = true, false

	runErr := cmd.Run()
	out.flush()
	errOut.flush()
	if ctx.Err() != nil {
		return Response{}, ctx.Err()
	}
	if runCtx.Err() != nil {
		return Response{}, ErrTransient{Cause: errors.New("cline: timed out")}
	}

	exited := 0
	var ee *exec.ExitError
	switch {
	case errors.As(runErr, &ee):
		exited = ee.ExitCode()
	case runErr != nil:
		return Response{}, ErrTransient{Cause: errors.New("cline: could not run the binary")}
	}
	res := out.res
	failed := exited != 0 || res == nil || (res.FinishReason != "" && res.FinishReason != "completed")
	if failed {
		texts := append(out.failTexts(), errOut.errTexts...)
		return Response{}, c.failure(texts, exited, res != nil)
	}

	src, err := readClineTarget(work, req.TargetFile)
	if err != nil {
		return Response{}, err
	}
	for _, line := range strings.Split(src, "\n") {
		if strings.TrimSpace(line) == "```" {
			return Response{}, ErrTransient{Cause: errors.New("cline: the target file contains a code fence")}
		}
	}
	return Response{
		Text:     "```go\n" + src + "```",
		Usage:    Usage{PromptTokens: res.Usage.InputTokens, CompletionTokens: res.Usage.OutputTokens},
		Model:    req.Model,
		Duration: time.Since(start),
	}, nil
}

func validateClineRequest(req Request) error {
	if !filepath.IsAbs(req.Workdir) {
		return errors.New("cline: the workdir must be an absolute path")
	}
	if fi, err := os.Stat(req.Workdir); err != nil || !fi.IsDir() {
		return errors.New("cline: the workdir is not an existing directory")
	}
	t := req.TargetFile
	if t == "" || t == "." || strings.ContainsAny(t, "\\\x00") || strings.HasPrefix(t, "/") || path.Clean(t) != t || t == ".." || strings.HasPrefix(t, "../") {
		return errors.New("cline: the target file must be a clean relative path inside the workdir")
	}
	if req.Model == "" {
		return errors.New("cline: no model given")
	}
	return nil
}

// removeClineTemp deletes the scratch directory, after checking it is the one
// MkdirTemp made: absolute, named with our prefix, directly under the temp dir.
func removeClineTemp(dir string) {
	target := dir
	if target == "" || !filepath.IsAbs(target) ||
		!strings.HasPrefix(filepath.Base(target), clineTempPrefix) ||
		filepath.Dir(target) != filepath.Clean(os.TempDir()) {
		return
	}
	os.RemoveAll(target)
}

func clineChildEnv() []string {
	env := []string{}
	for _, k := range clineEnv {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// copyTree copies the regular files of src into dst, leaving out any .git entry
// and every symlink, and never reading more than maxBytes or maxFiles.
func copyTree(src, dst string, maxBytes int64, maxFiles int) error {
	var bytesCopied int64
	files := 0
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.New("cline: could not read the workdir")
		}
		if p == src {
			return nil
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return errors.New("cline: could not read the workdir")
		}
		fi, err := d.Info()
		if err != nil {
			return errors.New("cline: could not read the workdir")
		}
		switch {
		case fi.IsDir():
			return os.MkdirAll(filepath.Join(dst, rel), fi.Mode().Perm()|0o700)
		case !fi.Mode().IsRegular():
			return nil
		}
		files++
		if files > maxFiles {
			return fmt.Errorf("cline: the repository copy exceeds the limit of %d files", maxFiles)
		}
		bytesCopied += fi.Size()
		if bytesCopied > maxBytes {
			return fmt.Errorf("cline: the repository copy exceeds the limit of %d bytes", maxBytes)
		}
		return copyFile(p, filepath.Join(dst, rel), fi.Mode().Perm())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return errors.New("cline: could not read the workdir")
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode|0o200)
	if err != nil {
		return errors.New("cline: could not write the scratch copy")
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return errors.New("cline: could not write the scratch copy")
	}
	if err := out.Close(); err != nil {
		return errors.New("cline: could not write the scratch copy")
	}
	return os.Chmod(dst, mode)
}

// readClineTarget returns the target file's text, ending in exactly one newline.
// It refuses anything that is not a regular file inside work, symlinks included.
func readClineTarget(work, target string) (string, error) {
	missing := ErrTransient{Cause: errors.New("cline: the target file was not written")}
	full := filepath.Join(work, filepath.FromSlash(target))
	realWork, err1 := filepath.EvalSymlinks(work)
	realFull, err2 := filepath.EvalSymlinks(full)
	if err1 != nil || err2 != nil || !strings.HasPrefix(realFull, realWork+string(filepath.Separator)) {
		return "", missing
	}
	if fi, err := os.Lstat(full); err != nil || !fi.Mode().IsRegular() {
		return "", missing
	}
	f, err := os.OpenFile(full, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", missing
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return "", missing
	}
	b, err := io.ReadAll(io.LimitReader(f, clineMaxTarget+1))
	if err != nil {
		return "", missing
	}
	if len(b) > clineMaxTarget {
		return "", ErrTransient{Cause: errors.New("cline: the target file is too large")}
	}
	s := strings.TrimRight(string(b), " \t\r\n")
	if s == "" {
		return "", missing
	}
	return s + "\n", nil
}

// failure maps what Cline said to a typed error. The prompt is never part of it.
func (c *Cline) failure(texts []string, exited int, haveResult bool) error {
	lower := strings.ToLower(strings.Join(texts, "\n"))
	for _, k := range []string{"unauthorized", "re-authenticate", "insufficient balance", "authentication", "api key"} {
		if strings.Contains(lower, k) {
			return ErrAuth{Provider: c.cfg.Name}
		}
	}
	for _, k := range []string{"rate limit", "too many requests", "429"} {
		if strings.Contains(lower, k) {
			return ErrRateLimited{}
		}
	}
	short := ""
	for _, t := range texts {
		if short = strings.Join(strings.Fields(t), " "); short != "" {
			break
		}
	}
	if home, err := os.UserHomeDir(); err == nil && len(home) > 1 {
		short = strings.ReplaceAll(short, home, "~")
	}
	if r := []rune(short); len(r) > 200 {
		short = string(r[:200])
	}
	switch {
	case short != "":
	case exited != 0:
		short = "exited with status " + strconv.Itoa(exited)
	case !haveResult:
		short = "no result"
	default:
		short = "the run did not complete"
	}
	return ErrTransient{Cause: fmt.Errorf("cline: %s", short)}
}

// clineLines is an io.Writer that parses newline-delimited JSON as it arrives
// and keeps only the events the provider needs, never the content stream.
type clineLines struct {
	stdout   bool
	buf      []byte
	overflow bool
	res      *clineResult
	doneText string
	doneBad  bool
	errTexts []string
}

type clineResult struct {
	FinishReason string `json:"finishReason"`
	Text         string `json:"text"`
	Usage        struct {
		InputTokens  int `json:"inputTokens"`
		OutputTokens int `json:"outputTokens"`
	} `json:"usage"`
}

func (l *clineLines) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			if len(l.buf)+len(p) > clineMaxLine {
				l.overflow, l.buf = true, l.buf[:0]
			} else if !l.overflow {
				l.buf = append(l.buf, p...)
			}
			break
		}
		if !l.overflow && len(l.buf)+i <= clineMaxLine {
			l.buf = append(l.buf, p[:i]...)
			l.line(l.buf)
		}
		l.buf, l.overflow = l.buf[:0], false
		p = p[i+1:]
	}
	return n, nil
}

func (l *clineLines) flush() {
	if !l.overflow && len(l.buf) > 0 {
		l.line(l.buf)
	}
	l.buf = nil
}

func (l *clineLines) line(b []byte) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '{' {
		return
	}
	var m struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Event   struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
			Text   string `json:"text"`
			Error  struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"event"`
	}
	if json.Unmarshal(b, &m) != nil {
		return
	}
	switch {
	case !l.stdout && m.Type == "error":
		l.errTexts = append(l.errTexts, m.Message)
	case !l.stdout:
	case m.Type == "run_result":
		var r clineResult
		if json.Unmarshal(b, &r) == nil {
			l.res = &r
		}
	case m.Type == "agent_event" && m.Event.Type == "error":
		l.errTexts = append(l.errTexts, m.Event.Error.Message, m.Event.Text)
	case m.Type == "agent_event" && m.Event.Type == "done":
		l.doneText, l.doneBad = m.Event.Text, m.Event.Reason != "completed"
	}
}

// failTexts lists the texts that describe a failed run, most specific first.
func (l *clineLines) failTexts() []string {
	var t []string
	if l.res != nil {
		t = append(t, l.res.Text)
	}
	if l.doneBad {
		t = append(t, l.doneText)
	}
	return append(t, l.errTexts...)
}
