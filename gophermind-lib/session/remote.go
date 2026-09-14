package session

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PushRemote uploads a saved session to a remote store (PUT baseURL/<id>.jsonl),
// so a team can share sessions across machines.
func PushRemote(id, baseURL string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return pushRemoteIn(dir, id, baseURL)
}

// PullRemote downloads a session from the remote store into the local store.
func PullRemote(id, baseURL string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return pullRemoteIn(dir, id, baseURL)
}

func remoteURL(baseURL, id string) string {
	return strings.TrimRight(baseURL, "/") + "/" + id + ".jsonl"
}

func pushRemoteIn(dir, id, baseURL string) error {
	if err := validID(id); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".jsonl"))
	if err != nil {
		return fmt.Errorf("session %q not found", id)
	}
	req, err := http.NewRequest(http.MethodPut, remoteURL(baseURL, id), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Type", "application/x-ndjson")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("remote store returned %d on push", resp.StatusCode)
	}
	return nil
}

func pullRemoteIn(dir, id, baseURL string) error {
	if err := validID(id); err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Get(remoteURL(baseURL, id))
	if err != nil {
		return fmt.Errorf("pull: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("session %q not found on remote (status %d)", id, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id+".jsonl"), data, 0o600)
}
