package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ConfigFileName is the global configuration file, a flat JSON object living in
// Dir(). JSON (rather than the .env it replaced) so the file is editable by hand
// with an obvious structure and by tooling without a bespoke parser.
const ConfigFileName = "config.json"

// Dir returns gophermind's global configuration directory, which also holds the
// session store, prompt history, and the other per-user state files.
//
// Resolution order:
//
//	GOPHERMIND_CONFIG_DIR  >  $HOME/.gophermind  >  <os user config dir>/gophermind
//
// The home-relative default is deliberate: it is the path a user can type, and
// it is the same on every platform.
func Dir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("GOPHERMIND_CONFIG_DIR")); dir != "" {
		return dir, nil
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".gophermind"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gophermind"), nil
}

// ConfigFilePath returns the path to the global config file (see Dir), read by
// Load as a gap-filler and written by the setup wizard.
func ConfigFilePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ConfigFileName), nil
}

// GlobalConfigExists reports whether the global config file has been written. It
// is the "already configured" signal for the first-run wizard trigger.
func GlobalConfigExists() bool {
	p, err := ConfigFilePath()
	if err != nil {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// envKeyFor maps a config.json key to the environment variable it sets.
//
// A key containing any lowercase letter is a friendly short name: it is
// uppercased and given the GOPHERMIND_ prefix, so "base_url" sets
// GOPHERMIND_BASE_URL. A key that is already all-caps is used verbatim, which is
// both the escape hatch for writing GOPHERMIND_* in full and the way non-prefixed
// variables such as GITHUB_TOKEN are expressed.
func envKeyFor(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if key == strings.ToUpper(key) {
		return key
	}
	return "GOPHERMIND_" + strings.ToUpper(key)
}

// fileKeyFor is envKeyFor's inverse, choosing how a variable is stored in
// config.json: GOPHERMIND_* becomes its friendly short name, anything else is
// stored verbatim. envKeyFor(fileKeyFor(k)) == k for every k.
func fileKeyFor(envKey string) string {
	envKey = strings.TrimSpace(envKey)
	if rest, ok := strings.CutPrefix(envKey, "GOPHERMIND_"); ok && rest != "" {
		return strings.ToLower(rest)
	}
	return envKey
}

// jsonScalar renders a config.json value as the string its environment variable
// takes. Arrays become the comma-separated form the list-valued variables
// already use; objects and null are rejected so a malformed edit is loud rather
// than silently ignored.
func jsonScalar(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case bool:
		return strconv.FormatBool(t), nil
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), nil
	case []any:
		parts := make([]string, 0, len(t))
		for _, el := range t {
			s, err := jsonScalar(el)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, ","), nil
	default:
		return "", fmt.Errorf("unsupported value type %T", v)
	}
}

// loadConfigFile reads the global config.json and sets any variable it names
// that is not already present in the process environment. Real (already
// exported) environment variables always win — the file only fills gaps — so a
// deployment's real config can never be overridden by a stale file. A missing
// file is not an error; a malformed one is.
func loadConfigFile(path string) error {
	values, err := readConfigFile(path)
	if err != nil {
		return err
	}
	for _, kv := range values {
		if _, present := os.LookupEnv(kv[0]); present {
			continue
		}
		if err := os.Setenv(kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// readConfigFile parses the config file at path into environment-variable
// {key, value} pairs, sorted by key. A missing file yields no pairs and no
// error.
func readConfigFile(path string) ([][2]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	pairs := make([][2]string, 0, len(raw))
	for k, v := range raw {
		key := envKeyFor(k)
		if key == "" || v == nil {
			continue
		}
		s, err := jsonScalar(v)
		if err != nil {
			return nil, fmt.Errorf("parse %s: key %q: %w", path, k, err)
		}
		pairs = append(pairs, [2]string{key, s})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i][0] < pairs[j][0] })
	return pairs, nil
}

// Save merges GOPHERMIND_* (and other environment-shaped) pairs into the config
// file at path and writes it atomically, creating the parent directory 0700 and
// the file 0600 (it may hold an API key).
//
// It is a MERGE, not a rewrite: keys the caller does not mention — including
// ones added by hand — are preserved, so re-running the setup wizard cannot
// silently drop hand-edited settings. A pair with an empty value removes its
// key, which is how a setting is unset.
func Save(path string, pairs [][2]string) error {
	existing, err := readConfigFile(path)
	if err != nil {
		return err
	}

	out := map[string]string{}
	for _, kv := range existing {
		out[kv[0]] = kv[1]
	}
	for _, kv := range pairs {
		key := strings.TrimSpace(kv[0])
		if key == "" {
			continue
		}
		if kv[1] == "" {
			delete(out, key)
			continue
		}
		out[key] = kv[1]
	}

	// json.Marshal sorts map keys, so the file has a stable order and diffs
	// cleanly across saves.
	doc := make(map[string]string, len(out))
	for k, v := range out {
		doc[fileKeyFor(k)] = v
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// legacyDirName is where releases before the config.json move kept the global
// .env and every sibling state file (sessions/, history, devices.json, ...).
func legacyDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gophermind"), nil
}

// Migrate performs the one-time move from the pre-config.json layout
// (<os user config dir>/gophermind, holding a .env) to Dir(), converting the
// .env into config.json. It reports whether anything was moved.
//
// It is a no-op when GOPHERMIND_CONFIG_DIR pins the location explicitly, when
// the new directory already has a config.json, or when there is no legacy
// directory. Files already present at the destination are left alone rather than
// overwritten, so a partial migration can be re-run safely.
//
// Every sibling state file moves too, not just the config: sessions, prompt
// history and device tokens are all resolved relative to Dir(), so moving the
// config alone would orphan them.
func Migrate() (bool, error) {
	if strings.TrimSpace(os.Getenv("GOPHERMIND_CONFIG_DIR")) != "" {
		return false, nil
	}
	newDir, err := Dir()
	if err != nil {
		return false, err
	}
	oldDir, err := legacyDir()
	if err != nil {
		return false, nil // no legacy location to migrate from
	}
	if oldDir == newDir {
		return false, nil
	}
	if _, err := os.Stat(filepath.Join(newDir, ConfigFileName)); err == nil {
		return false, nil // already migrated
	}
	entries, err := os.ReadDir(oldDir)
	if err != nil || len(entries) == 0 {
		return false, nil // nothing to migrate
	}

	if err := os.MkdirAll(newDir, 0o700); err != nil {
		return false, fmt.Errorf("create config dir: %w", err)
	}
	moved := false
	for _, e := range entries {
		dst := filepath.Join(newDir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue // destination wins
		}
		if err := os.Rename(filepath.Join(oldDir, e.Name()), dst); err != nil {
			return moved, fmt.Errorf("move %s: %w", e.Name(), err)
		}
		moved = true
	}

	// Convert the legacy .env into config.json, then drop it so there is exactly
	// one source of truth.
	envPath := filepath.Join(newDir, ".env")
	if pairs, perr := parseDotEnv(envPath); perr == nil && pairs != nil {
		if err := Save(filepath.Join(newDir, ConfigFileName), pairs); err != nil {
			return moved, err
		}
		if err := os.Remove(envPath); err != nil {
			return moved, err
		}
		moved = true
	}

	// Tidy up the now-empty legacy directory. os.Remove only succeeds on an
	// empty one, so anything left behind (a file the destination already had) is
	// preserved rather than deleted.
	os.Remove(oldDir)
	return moved, nil
}
