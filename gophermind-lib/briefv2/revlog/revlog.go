// Package revlog is the record of the rewrites of one function node: the
// staged bodies and tests of each revision, an append-only log, and the hash
// chain that lets the executor prove an edited node file is a recorded,
// valid revision and not a hand edit (planner revise stage design, sections 8
// to 10). It writes only below <run>/_state/revisions/<node>/ and holds no
// prompt, no reply and no secret: ids, hashes, field names and fixed classes.
package revlog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"gophermind/gophermind-lib/briefv2/runfs"
)

// DirName is the folder below the run folder that holds one folder per revised node.
const DirName = "_state/revisions"

// Kind is the type of a log record.
type Kind string

const (
	KindIntent  Kind = "intent"  // a revision is about to be applied; recovery rolls it forward
	KindCommit  Kind = "commit"  // the revision is applied
	KindFlag    Kind = "flag"    // a contract_flagged or split_required outcome: no body change
	KindRefused Kind = "refused" // a rejected reply or test; with N it closes the intent of that N
)

// Record is one line of log.jsonl.
type Record struct {
	Kind             Kind     `json:"kind"`
	N                int      `json:"n,omitempty"`
	Trigger          string   `json:"trigger,omitempty"` // ladder_exhausted or contract_problem; the model's sentence is never written
	BaseSHA256       string   `json:"base_sha256,omitempty"`
	BodySHA256       string   `json:"body_sha256,omitempty"`
	TestSHA256Before string   `json:"test_sha256_before,omitempty"`
	TestSHA256After  string   `json:"test_sha256_after,omitempty"`
	Changed          []string `json:"changed,omitempty"` // field names only
	Assumptions      []string `json:"assumptions,omitempty"`
	Model            string   `json:"model,omitempty"`
	Class            string   `json:"class,omitempty"`
	// Round is set on a commit record: the leaf's human-retry round when the
	// revision was committed. With the node id and N it keys the one revision
	// escalation a committed rewrite counts, so a replay never adds another.
	Round int    `json:"round,omitempty"`
	At    string `json:"at"`
}

// ErrChain is wrapped by every Verify failure. The text names a rule, never content.
var ErrChain = errors.New("revlog: the revision record does not match the node")

func chainErr(rule string) error { return fmt.Errorf("%w (%s)", ErrChain, rule) }

// Hash is the SHA-256 of b in hex, the same function the planner and executor use.
// Canonical bytes: every hash in this package is over the exact bytes of the
// file as stored (node JSON as written, test source as written), never over a
// re-encoding, so a hash can be reproduced with sha256sum on the file.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// NodeDir is the folder of one node's revisions. The id must be a plain node
// id; a link at _state, at revisions or at the node folder is refused.
func NodeDir(runDir, id string) (string, error) {
	if !idRE.MatchString(id) {
		return "", errors.New("revlog: invalid node id")
	}
	for _, p := range []string{
		filepath.Join(runDir, "_state"),
		filepath.Join(runDir, filepath.FromSlash(DirName)),
		filepath.Join(runDir, filepath.FromSlash(DirName), id),
	} {
		if fi, err := os.Lstat(p); err == nil && (fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir()) {
			return "", errors.New("revlog: a revision folder is not a plain directory")
		}
	}
	return filepath.Join(runDir, filepath.FromSlash(DirName), id), nil
}

// BodyFile and TestFile are the staged file names of revision k (0 = as approved).
func BodyFile(k int) string { return fmt.Sprintf("body-%d.json", k) }
func TestFile(k int) string { return fmt.Sprintf("test-%d.txt", k) }

const logName = "log.jsonl"

// Bounds on what a record may hold, so a log line stays small and a model
// sentence cannot be smuggled into it.
const (
	maxHashLen       = 64   // a hex SHA-256
	maxTokenLen      = 128  // model, class, trigger, a changed field name
	maxAssumptionLen = 1024 // one "revision n: ..." assumption
	maxListLen       = 64   // entries in Changed or Assumptions
)

var errTooLong = errors.New("revlog: a record field is over its length bound")

func checkBounds(r Record) error {
	for _, v := range []string{r.BaseSHA256, r.BodySHA256, r.TestSHA256Before, r.TestSHA256After} {
		if len(v) > maxHashLen {
			return errTooLong
		}
	}
	for _, v := range []string{r.Trigger, r.Model, r.Class, r.At} {
		if len(v) > maxTokenLen {
			return errTooLong
		}
	}
	if len(r.Changed) > maxListLen || len(r.Assumptions) > maxListLen {
		return errTooLong
	}
	for _, v := range r.Changed {
		if len(v) > maxTokenLen {
			return errTooLong
		}
	}
	for _, v := range r.Assumptions {
		if len(v) > maxAssumptionLen {
			return errTooLong
		}
	}
	return nil
}

// plainDir refuses a path that exists and is a link or not a directory.
func plainDir(dir string) error {
	if fi, err := os.Lstat(dir); err == nil && (fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir()) {
		return errors.New("revlog: a revision folder is not a plain directory")
	}
	return nil
}

// plainFile refuses a path that exists and is not a regular file (a link, say).
func plainFile(path string) error {
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return errors.New("revlog: a revision file is not a regular file")
	}
	return nil
}

// Append writes one record. The folder is made 0700 and the file 0600. A log
// that ends in a torn line (no newline) is first cut back to its last complete
// line: Read never returned that stub, so the append it was part of never
// happened, and the new record must not be glued onto it.
func Append(dir string, r Record) error {
	switch r.Kind {
	case KindIntent, KindCommit, KindFlag, KindRefused:
	default:
		return errors.New("revlog: unknown record kind")
	}
	if err := checkBounds(r); err != nil {
		return err
	}
	line, err := json.Marshal(r)
	if err != nil {
		return errors.New("revlog: encoding a record failed")
	}
	if err := plainDir(dir); err != nil {
		return err
	}
	path := filepath.Join(dir, logName)
	if err := plainFile(path); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("revlog: creating the revision folder failed")
	}
	if err := cutTornTail(path); err != nil {
		return err
	}
	return runfs.AppendLine(path, line)
}

func cutTornTail(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && (len(data) == 0 || data[len(data)-1] == '\n')) {
		return nil
	}
	if err != nil {
		return errors.New("revlog: the log is not readable")
	}
	if err := os.Truncate(path, int64(bytes.LastIndexByte(data, '\n')+1)); err != nil {
		return errors.New("revlog: repairing the log tail failed")
	}
	return nil
}

// Read returns the records in order. A record with an unknown field, or a line
// that is not JSON, is an error. A last line without its newline (a torn append)
// is not returned: an append is one write, so it never happened.
func Read(dir string) ([]Record, error) {
	if err := plainDir(dir); err != nil {
		return nil, err
	}
	if err := plainFile(filepath.Join(dir, logName)); err != nil {
		return nil, err
	}
	lines, _, err := runfs.ReadLines(filepath.Join(dir, logName), 0)
	if err != nil {
		return nil, errors.New("revlog: the log is not readable")
	}
	var out []Record
	for _, ln := range lines {
		var r Record
		dec := json.NewDecoder(bytes.NewReader(ln))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&r); err != nil {
			return nil, chainErr("a log line is not a record")
		}
		if _, err := dec.Token(); err != io.EOF {
			return nil, chainErr("a log line holds more than a record")
		}
		out = append(out, r)
	}
	return out, nil
}

// Stage writes body-k.json and test-k.txt atomically (0600). For k >= 1 it
// replaces what is there; the caller never stages k while an intent for k is
// open. Revision 0 is the approved plan and is create-only: a complete pair is
// never replaced. A half pair (a kill or a full disk between the two writes)
// is replaced only while the log holds no intent, since every intent depends
// on revision 0. A link in the way is refused.
func Stage(dir string, k int, body, test []byte) error {
	if err := plainDir(dir); err != nil {
		return err
	}
	present := 0
	for _, n := range []string{BodyFile(k), TestFile(k)} {
		path := filepath.Join(dir, n)
		if err := plainFile(path); err != nil {
			return err
		}
		if _, err := os.Lstat(path); err == nil {
			present++
		}
	}
	if k == 0 && present == 2 {
		return errors.New("revlog: revision 0 is already staged")
	}
	if k == 0 && present == 1 {
		recs, err := Read(dir)
		if err != nil {
			return err
		}
		for _, r := range recs {
			if r.Kind == KindIntent {
				return errors.New("revlog: revision 0 is already staged")
			}
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("revlog: creating the revision folder failed")
	}
	if err := runfs.WriteFileAtomic(filepath.Join(dir, BodyFile(k)), body); err != nil {
		return errors.New("revlog: staging a body failed")
	}
	if err := runfs.WriteFileAtomic(filepath.Join(dir, TestFile(k)), test); err != nil {
		return errors.New("revlog: staging a test failed")
	}
	return nil
}

func readRegular(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	return os.ReadFile(path)
}

// ReadStaged returns the staged body and test of revision k.
func ReadStaged(dir string, k int) (body, test []byte, err error) {
	if body, err = readRegular(filepath.Join(dir, BodyFile(k))); err != nil {
		return nil, nil, chainErr("a staged body is missing")
	}
	if test, err = readRegular(filepath.Join(dir, TestFile(k))); err != nil {
		return nil, nil, chainErr("a staged test is missing")
	}
	return body, test, nil
}

// RemoveStaged deletes the staged files of revision k (a refused revision). Missing files are fine.
func RemoveStaged(dir string, k int) error {
	for _, n := range []string{BodyFile(k), TestFile(k)} {
		if err := os.Remove(filepath.Join(dir, n)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("revlog: removing a staged file failed")
		}
	}
	return nil
}

// Disk is what is on disk now: the hash of the node file, the sha256 field of
// the node's entry in _state/leaf_tests.json, and the hash of the test file.
type Disk struct{ NodeSHA256, LeafTestSHA256, TestFileSHA256 string }

// Tip is the state of the chain: the last committed revision (0 when none),
// its body and test hashes, and the intent that is open, if any.
type Tip struct {
	N                      int
	BodySHA256, TestSHA256 string
	Open                   *Record
}

// Verify proves the chain (spec section 8 point 3) against what is on disk.
// A dir with only flag and refused records, and no intent, is revision 0 and
// needs no staged file. Every failure wraps ErrChain.
func Verify(dir string, d Disk) (Tip, error) {
	recs, err := Read(dir)
	if err != nil {
		return Tip{}, err
	}
	anyIntent := false
	for _, r := range recs {
		anyIntent = anyIntent || r.Kind == KindIntent
	}
	var body0, test0 []byte
	tip := Tip{}
	if anyIntent {
		if body0, test0, err = ReadStaged(dir, 0); err != nil {
			return Tip{}, err
		}
		tip = Tip{BodySHA256: Hash(body0), TestSHA256: Hash(test0)}
	}
	var open *Record
	for i := range recs {
		r := recs[i]
		switch r.Kind {
		case KindIntent:
			if open != nil || r.N != tip.N+1 {
				return Tip{}, chainErr("an intent is out of order")
			}
			if r.BaseSHA256 != tip.BodySHA256 || r.TestSHA256Before != tip.TestSHA256 {
				return Tip{}, chainErr("an intent does not extend the chain")
			}
			open = &recs[i]
		case KindCommit:
			if open == nil || open.N != r.N || r.BaseSHA256 != open.BaseSHA256 || r.BodySHA256 != open.BodySHA256 ||
				r.TestSHA256Before != open.TestSHA256Before || r.TestSHA256After != open.TestSHA256After {
				return Tip{}, chainErr("a commit has no matching intent")
			}
			if err := checkStaged(dir, r); err != nil {
				return Tip{}, err
			}
			tip = Tip{N: r.N, BodySHA256: r.BodySHA256, TestSHA256: r.TestSHA256After}
			open = nil
		case KindRefused:
			if open != nil && open.N == r.N {
				open = nil
			}
		case KindFlag:
		default:
			return Tip{}, chainErr("an unknown record kind")
		}
	}
	tip.Open = open
	if open != nil {
		if err := checkStaged(dir, *open); err != nil {
			return Tip{}, err
		}
	}
	if open == nil {
		if tip.N == 0 && !anyIntent {
			return tip, nil
		}
		if d.NodeSHA256 != tip.BodySHA256 || d.LeafTestSHA256 != tip.TestSHA256 || d.TestFileSHA256 != tip.TestSHA256 {
			return Tip{}, chainErr("the files on disk are not the chain tip")
		}
	} else {
		if d.NodeSHA256 != open.BaseSHA256 && d.NodeSHA256 != open.BodySHA256 {
			return Tip{}, chainErr("the node file is neither the base nor the open revision")
		}
		for _, h := range []string{d.LeafTestSHA256, d.TestFileSHA256} {
			if h != open.TestSHA256Before && h != open.TestSHA256After {
				return Tip{}, chainErr("a test is neither the base nor the open revision")
			}
		}
	}
	// Frozen fields hold for the tip and for the body of an open intent.
	for _, k := range []int{tip.N, openN(open)} {
		if k < 1 {
			continue
		}
		last, _, err := ReadStaged(dir, k)
		if err != nil {
			return Tip{}, err
		}
		if err := CheckFrozen(body0, last); err != nil {
			return Tip{}, fmt.Errorf("%w (%v)", ErrChain, err)
		}
	}
	return tip, nil
}

func openN(r *Record) int {
	if r == nil {
		return 0
	}
	return r.N
}

// checkStaged proves the staged files of an intent are the ones it names.
func checkStaged(dir string, r Record) error {
	body, test, err := ReadStaged(dir, r.N)
	if err != nil {
		return err
	}
	if Hash(body) != r.BodySHA256 || Hash(test) != r.TestSHA256After {
		return chainErr("a staged file does not match its record")
	}
	return nil
}

// FrozenError says which part of a node changed that a revision may not change.
type FrozenError struct{ Field string }

func (e *FrozenError) Error() string { return "revlog: the frozen field " + e.Field + " changed" }

var fieldRE = regexp.MustCompile(`^[a-z_.]{1,48}$`)

// What a revision may change (spec section 7). Everything else is frozen.
var (
	mayChange        = map[string]bool{"description": true, "tests": true, "construction": true, "security": true, "observability": true, "performance": true, "portability": true, "profile_hooks": true, "assumptions": true, "revision": true}
	mayChangeContext = map[string]bool{"constraints": true, "notes": true}
	mayChangeContrct = map[string]bool{"inputs": true, "outputs": true, "errors": true, "side_effects": true}
)

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// CheckFrozen compares two node documents and returns a *FrozenError for the
// first frozen field that differs. The allowed fields are the ones the revise
// stage may rewrite; every other top-level field, and every other field of
// contract and context, must be equal (so signature, package, file,
// depends_on, wave, model_tier, rationale and the rest cannot move).
func CheckFrozen(base, now []byte) error {
	var a, b map[string]any
	if json.Unmarshal(base, &a) != nil || json.Unmarshal(now, &b) != nil {
		return &FrozenError{Field: "document"}
	}
	name := func(parent, key string) string {
		f := key
		if parent != "" {
			f = parent + "." + key
		}
		if !fieldRE.MatchString(f) {
			return "unknown"
		}
		return f
	}
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		switch k {
		case "contract", "context":
			allow := mayChangeContrct
			if k == "context" {
				allow = mayChangeContext
			}
			ma, okA := a[k].(map[string]any)
			mb, okB := b[k].(map[string]any)
			if !okA || !okB {
				// Not an object on one side: nothing inside to allow, so the whole value is frozen.
				if !sameJSON(a[k], b[k]) {
					return &FrozenError{Field: name("", k)}
				}
				continue
			}
			subs := map[string]bool{}
			for s := range ma {
				subs[s] = true
			}
			for s := range mb {
				subs[s] = true
			}
			ss := make([]string, 0, len(subs))
			for s := range subs {
				ss = append(ss, s)
			}
			sort.Strings(ss)
			for _, s := range ss {
				if !allow[s] && !sameJSON(ma[s], mb[s]) {
					return &FrozenError{Field: name(k, s)}
				}
			}
		default:
			if !mayChange[k] && !sameJSON(a[k], b[k]) {
				return &FrozenError{Field: name("", k)}
			}
		}
	}
	return nil
}

// Summary is what the report says about one node's revisions.
type Summary struct {
	Commits        int
	Changed        []string // union of the changed field names, in first-seen order
	TestsRewritten bool
	Flag           string // class of the last flag record
	Refusals       []string
}

// Summarize reads records already returned by Read.
func Summarize(recs []Record) Summary {
	var s Summary
	seen := map[string]bool{}
	for _, r := range recs {
		switch r.Kind {
		case KindCommit:
			s.Commits++
			for _, c := range r.Changed {
				if !seen[c] {
					seen[c] = true
					s.Changed = append(s.Changed, c)
				}
			}
			if r.TestSHA256After != r.TestSHA256Before {
				s.TestsRewritten = true
			}
		case KindFlag:
			s.Flag = r.Class
		case KindRefused:
			s.Refusals = append(s.Refusals, r.Class)
		}
	}
	return s
}
