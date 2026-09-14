package llm

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
)

// cassetteEntry is one recorded request/response interaction.
type cassetteEntry struct {
	Key    string `json:"key"`
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// requestKey derives a stable key from an outgoing request's method, URL, and
// body, so the same logical request replays to the same recorded response.
func requestKey(method, url string, body []byte) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00", method, url)
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// readAndRestore reads a request body fully and returns it, restoring req.Body
// so the request can still be sent (or matched) downstream.
func readAndRestore(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// recorder is a RoundTripper that delegates to a base transport and appends each
// interaction to a cassette file, so a real session can be captured once and
// replayed offline. It reads the response body (fine for non-streaming Complete
// calls; do not use it on streaming requests).
type recorder struct {
	base http.RoundTripper
	path string
	mu   sync.Mutex
}

// NewRecorder wraps base, recording interactions to the cassette at path.
func NewRecorder(base http.RoundTripper, path string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &recorder{base: base, path: path}
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	reqBody, err := readAndRestore(req)
	if err != nil {
		return nil, err
	}
	resp, err := r.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	entry := cassetteEntry{
		Key:    requestKey(req.Method, req.URL.String(), reqBody),
		Status: resp.StatusCode,
		Body:   string(respBody),
	}
	if err := r.append(entry); err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	return resp, nil
}

func (r *recorder) append(e cassetteEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(e)
	_, err = f.Write(append(b, '\n'))
	return err
}

// replayer is a RoundTripper that serves responses from a cassette by request
// key, never touching the network — for hermetic, deterministic agent tests.
type replayer struct {
	entries map[string]cassetteEntry
}

// NewReplayer loads a cassette and returns a RoundTripper that replays it.
func NewReplayer(path string) (http.RoundTripper, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries := map[string]cassetteEntry{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e cassetteEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("parse cassette: %w", err)
		}
		entries[e.Key] = e
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return &replayer{entries: entries}, nil
}

func (r *replayer) RoundTrip(req *http.Request) (*http.Response, error) {
	reqBody, err := readAndRestore(req)
	if err != nil {
		return nil, err
	}
	key := requestKey(req.Method, req.URL.String(), reqBody)
	e, ok := r.entries[key]
	if !ok {
		return nil, fmt.Errorf("no recorded response for %s %s (cassette miss)", req.Method, req.URL)
	}
	return &http.Response{
		StatusCode: e.Status,
		Status:     fmt.Sprintf("%d", e.Status),
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(e.Body))),
		Request:    req,
	}, nil
}
