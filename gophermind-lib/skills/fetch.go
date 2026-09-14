package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gophermind/gophermind-lib/gitenv"
)

// CacheDir returns where fetched skill repositories are stored, given the
// gophermind config directory.
func CacheDir(configDir string) string {
	return filepath.Join(configDir, "skill-cache")
}

// sourceDir is where one pinned commit of one repository lives. The SHA is in
// the directory name so two pins of the same repository can coexist while an
// update is being reviewed, and so a stale pin cannot be mistaken for the
// current one.
func sourceDir(cacheDir, id, sha string) string {
	return filepath.Join(cacheDir, filepath.FromSlash(id)+"@"+shortSHA(sha))
}

// destFor resolves where a source's content is installed and refuses any
// result that is not inside the cache.
//
// ValidateSourceURL already rejects the id that made this reachable, so this
// is the second lock rather than the first. It is here because the operations
// guarded are os.RemoveAll and os.Rename: a later change to SourceID, or a new
// caller that skips validation, should fail loudly rather than delete
// something outside the cache.
func destFor(cacheDir, id, sha string) (string, error) {
	root, err := filepath.Abs(cacheDir)
	if err != nil {
		return "", err
	}
	dest, err := filepath.Abs(sourceDir(cacheDir, id, sha))
	if err != nil {
		return "", err
	}
	if dest == root || !strings.HasPrefix(dest, root+string(filepath.Separator)) {
		return "", fmt.Errorf("source id %q resolves outside the skill cache", id)
	}
	return dest, nil
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// Fetch clones a skill repository at ref and returns the Source pinned to the
// commit it actually got.
//
// The clone is deliberately inert. Hooks are disabled, submodules are not
// followed, and the depth is one:
//
//   - a repository can ship hooks in .git, and while a fresh clone does not
//     run the remote's hooks, setting core.hooksPath costs one flag and
//     removes the question entirely
//   - a submodule would pull a second, unreviewed repository behind the one
//     the user actually named
//   - GIT_* is stripped from the environment, because an inherited GIT_DIR
//     redirects git at another repository; that is not hypothetical in this
//     project's history
//
// Nothing fetched is enabled by this call. Arriving on disk and being trusted
// are separate events.
func Fetch(ctx context.Context, cacheDir, rawURL, ref string) (Source, error) {
	if err := ValidateSourceURL(rawURL); err != nil {
		return Source{}, err
	}
	id := SourceID(rawURL)
	if id == "" {
		return Source{}, fmt.Errorf("could not derive owner/repo from %q", rawURL)
	}
	if ref == "" {
		ref = "HEAD"
	}

	tmp, err := os.MkdirTemp(cacheDir, ".fetch-")
	if err != nil {
		if mkErr := os.MkdirAll(cacheDir, 0o700); mkErr != nil {
			return Source{}, mkErr
		}
		if tmp, err = os.MkdirTemp(cacheDir, ".fetch-"); err != nil {
			return Source{}, err
		}
	}
	defer os.RemoveAll(tmp)

	args := []string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "protocol.ext.allow=never",
		"clone", "--depth", "1", "--no-tags", "--recurse-submodules=no", "--quiet",
	}
	if ref != "HEAD" {
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", rawURL, tmp)

	cmd := gitenv.CommandContext(ctx, cacheDir, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return Source{}, fmt.Errorf("clone %s: %w: %s", rawURL, err, strings.TrimSpace(string(out)))
	}

	shaOut, err := gitenv.CommandContext(ctx, tmp, "rev-parse", "HEAD").Output()
	if err != nil {
		return Source{}, fmt.Errorf("resolve commit: %w", err)
	}
	sha := strings.TrimSpace(string(shaOut))

	dest, err := destFor(cacheDir, id, sha)
	if err != nil {
		return Source{}, err
	}
	if err := os.RemoveAll(dest); err != nil {
		return Source{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return Source{}, err
	}
	// Drop .git: the cache holds reviewed content at a pinned commit, not a
	// working repository. Keeping it would let a later command in that
	// directory pull, check out, or run a configured hook.
	if err := os.RemoveAll(filepath.Join(tmp, ".git")); err != nil {
		return Source{}, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return Source{}, fmt.Errorf("install %s: %w", id, err)
	}

	return Source{URL: rawURL, Ref: ref, SHA: sha, Added: time.Now().UTC()}, nil
}

// Remove deletes every cached copy of a source and drops its skills from the
// enabled list, so removing a repository cannot leave its skills switched on.
func Remove(cacheDir string, s Settings, id string) (Settings, error) {
	var kept []Source
	for _, src := range s.Sources {
		if SourceID(src.URL) == id {
			if err := os.RemoveAll(sourceDir(cacheDir, id, src.SHA)); err != nil {
				return s, err
			}
			continue
		}
		kept = append(kept, src)
	}
	s.Sources = kept

	prefix := id + ":"
	var enabled []string
	for _, k := range s.Enabled {
		if !strings.HasPrefix(k, prefix) {
			enabled = append(enabled, k)
		}
	}
	s.Enabled = enabled
	return s, nil
}
