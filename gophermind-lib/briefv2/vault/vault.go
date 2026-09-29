// Package vault stores secrets in one age-encrypted file (scrypt passphrase).
// Two scopes share the file: "harness" (provider keys) and "run/<id>" (secrets
// a brief declared). Values are handed to commands only through Env, which
// returns a slice for exec.Cmd.Env and never touches the process environment.
package vault

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"filippo.io/age"
	"golang.org/x/term"
)

const (
	HarnessScope  = "harness"
	PassphraseEnv = "GOPHERMIND_VAULT_PASSPHRASE"
)

// RunScope is the scope for a brief's declared secrets.
func RunScope(id string) string { return "run/" + id }

// Options tunes the vault. WorkFactor 0 uses age's default (18); tests pass a
// small value so scrypt stays fast.
type Options struct{ WorkFactor int }

type Vault struct {
	path string
	pass string
	opts Options

	mu   sync.Mutex
	data map[string]map[string]string
}

var nameRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// Open decrypts the vault at path, or returns an empty one when the file does
// not exist yet. A wrong passphrase is an error and leaves the file untouched.
func Open(path, passphrase string, opts Options) (*Vault, error) {
	v := &Vault{path: path, pass: passphrase, opts: opts, data: map[string]map[string]string{}}
	enc, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return nil, fmt.Errorf("vault: read %s: %w", path, err)
	}
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	r, err := age.Decrypt(bytes.NewReader(enc), id)
	if err != nil {
		return nil, fmt.Errorf("vault: cannot decrypt %s (wrong passphrase?): %w", path, err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("vault: cannot decrypt %s: %w", path, err)
	}
	if err := json.Unmarshal(plain, &v.data); err != nil {
		return nil, fmt.Errorf("vault: %s is corrupt: %w", path, err)
	}
	return v, nil
}

// Set stores value under scope/name and persists the whole vault.
func (v *Vault) Set(scope, name, value string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("vault: secret name %q must match %s", name, nameRE)
	}
	if value == "" {
		return fmt.Errorf("vault: empty value for %s", name)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data[scope] == nil {
		v.data[scope] = map[string]string{}
	}
	old, had := v.data[scope][name]
	v.data[scope][name] = value
	if err := v.save(); err != nil {
		if had {
			v.data[scope][name] = old
		} else {
			delete(v.data[scope], name)
		}
		return err
	}
	return nil
}

func (v *Vault) Get(scope, name string) (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	s, ok := v.data[scope][name]
	return s, ok
}

// Names lists secret names in scope, sorted. Never values.
func (v *Vault) Names(scope string) []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]string, 0, len(v.data[scope]))
	for n := range v.data[scope] {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Env returns NAME=value pairs for exactly the named secrets in scope, for use
// as exec.Cmd.Env entries. A missing name is an error naming the secret only.
func (v *Vault) Env(scope string, names []string) ([]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]string, 0, len(names))
	for _, n := range names {
		val, ok := v.data[scope][n]
		if !ok {
			return nil, fmt.Errorf("vault: secret %s is not set in scope %s", n, scope)
		}
		out = append(out, n+"="+val)
	}
	return out, nil
}

// save must be called with v.mu held.
func (v *Vault) save() error {
	plain, err := json.Marshal(v.data)
	if err != nil {
		return err
	}
	rec, err := age.NewScryptRecipient(v.pass)
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	if v.opts.WorkFactor > 0 {
		rec.SetWorkFactor(v.opts.WorkFactor)
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, rec)
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	if _, err := w.Write(plain); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	dir := filepath.Dir(v.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".vault-*")
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), v.path)
}

// Passphrase returns GOPHERMIND_VAULT_PASSPHRASE when set, otherwise prompts
// on in with echo off.
func Passphrase(prompt string, in *os.File, out io.Writer) (string, error) {
	if v := os.Getenv(PassphraseEnv); v != "" {
		return v, nil
	}
	return ReadSecret(prompt, in, out)
}

// ReadSecret reads one secret with echo off when in is a terminal, or one line
// when in is a pipe (so `echo value | gophermind brief vault set NAME` works).
func ReadSecret(prompt string, in *os.File, out io.Writer) (string, error) {
	fd := int(in.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(out, prompt)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(out)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", fmt.Errorf("vault: no value on stdin and no terminal to prompt (set %s for the passphrase)", PassphraseEnv)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
