package safety

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// AuditEntry records a single tool-call event in a tamper-evident chain. Each
// entry's Hash covers its own fields plus the previous entry's Hash, so any
// after-the-fact edit, reorder, or deletion breaks the chain. The tool result
// is stored only as a hash (privacy + integrity, not disclosure).
type AuditEntry struct {
	Seq        int    `json:"seq"`
	Timestamp  string `json:"timestamp"`
	Tool       string `json:"tool"`
	Args       string `json:"args"`
	Decision   string `json:"decision"` // "approved" | "denied" | "auto"
	ResultHash string `json:"result_hash"`
	PrevHash   string `json:"prev_hash"`
	Hash       string `json:"hash"`
}

// AuditLog appends tamper-evident tool-call entries to a local JSONL file.
type AuditLog struct {
	mu      sync.Mutex
	path    string
	last    string // running chain head (previous entry's Hash)
	seq     int
	entries []AuditEntry
}

// NewAuditLog creates an audit log that appends to path (empty path = in-memory).
func NewAuditLog(path string) *AuditLog {
	return &AuditLog{path: path}
}

// Record appends a tool-call entry, chaining it to the previous one, and (when a
// path is set) appends it to the JSONL file. Nil-safe: a nil log is a no-op.
func (al *AuditLog) Record(tool, args, decision, result string) error {
	if al == nil {
		return nil
	}
	al.mu.Lock()
	defer al.mu.Unlock()

	al.seq++
	e := AuditEntry{
		Seq:        al.seq,
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		Tool:       tool,
		Args:       args,
		Decision:   decision,
		ResultHash: hashString(result),
		PrevHash:   al.last,
	}
	e.Hash = entryHash(e)
	al.last = e.Hash
	al.entries = append(al.entries, e)

	if al.path != "" {
		f, err := os.OpenFile(al.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		b, _ := json.Marshal(e)
		if _, err := f.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// Entries returns the in-memory entries recorded by this log.
func (al *AuditLog) Entries() []AuditEntry {
	al.mu.Lock()
	defer al.mu.Unlock()
	return al.entries
}

// entryHash computes the chained hash over an entry's fields (excluding Hash).
func entryHash(e AuditEntry) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s",
		e.Seq, e.PrevHash, e.Timestamp, e.Tool, e.Args, e.Decision, e.ResultHash)
	return hex.EncodeToString(h.Sum(nil))
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// VerifyAuditFile re-reads a JSONL audit file and checks the hash chain: each
// entry's recomputed Hash must match, and its PrevHash must equal the prior
// entry's Hash. A missing or empty file verifies trivially.
func VerifyAuditFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	prev := ""
	line := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line++
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal(raw, &e); err != nil {
			return fmt.Errorf("line %d: %w", line, err)
		}
		if e.PrevHash != prev {
			return fmt.Errorf("line %d: broken chain (prev_hash mismatch)", line)
		}
		want := e.Hash
		e.Hash = ""
		if entryHash(e) != want {
			return fmt.Errorf("line %d: tampered entry (hash mismatch)", line)
		}
		prev = want
	}
	return sc.Err()
}
