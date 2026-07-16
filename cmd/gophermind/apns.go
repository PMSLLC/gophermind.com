package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gophermind/internal/config"
)

// --- config ---

// apnsConfig holds the APNs settings read once from the environment. The
// pusher is disabled (no-op) whenever key path/id, team id, or bundle id is
// unset, so an unconfigured server never attempts push and never errors a
// turn over it.
type apnsConfig struct {
	keyPath  string
	keyID    string
	teamID   string
	bundleID string
	env      string // "sandbox" or "prod"
}

// loadAPNsConfig reads the GOPHERMIND_APNS_* environment variables.
func loadAPNsConfig() apnsConfig {
	return apnsConfig{
		keyPath:  strings.TrimSpace(os.Getenv("GOPHERMIND_APNS_KEY_P8")),
		keyID:    strings.TrimSpace(os.Getenv("GOPHERMIND_APNS_KEY_ID")),
		teamID:   strings.TrimSpace(os.Getenv("GOPHERMIND_APNS_TEAM_ID")),
		bundleID: strings.TrimSpace(os.Getenv("GOPHERMIND_APNS_BUNDLE_ID")),
		env:      apnsEnvFromEnv(),
	}
}

// apnsEnvFromEnv returns "prod" only when GOPHERMIND_APNS_ENV is exactly
// that (case-insensitive); everything else (including unset) is "sandbox".
func apnsEnvFromEnv() string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("GOPHERMIND_APNS_ENV")), "prod") {
		return "prod"
	}
	return "sandbox"
}

// enabled reports whether every setting needed to sign and target push
// requests is present.
func (c apnsConfig) enabled() bool {
	return c.keyPath != "" && c.keyID != "" && c.teamID != "" && c.bundleID != ""
}

// host returns the APNs HTTP/2 host for the configured environment.
func (c apnsConfig) host() string {
	if c.env == "prod" {
		return "api.push.apple.com"
	}
	return "api.sandbox.push.apple.com"
}

// --- device store ---

// deviceRecord is one registered iOS device, persisted as JSON.
type deviceRecord struct {
	Token    string `json:"device_token"`
	Platform string `json:"platform"`
}

// deviceStore holds the registered push tokens for the (single-user) serve
// process: a JSON file under the config dir, mutex-guarded, deduped by
// token. Mirrors internal/session's config-dir resolution.
type deviceStore struct {
	mu     sync.Mutex
	path   string
	tokens map[string]deviceRecord
}

// devicesFilePath returns devices.json next to the global .env, honoring
// GOPHERMIND_CONFIG_DIR the same way internal/session.Dir does.
func devicesFilePath() (string, error) {
	p, err := config.ConfigFilePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "devices.json"), nil
}

// newDeviceStore builds a deviceStore and loads any previously persisted
// tokens. A missing file is not an error (first run).
func newDeviceStore() (*deviceStore, error) {
	p, err := devicesFilePath()
	if err != nil {
		return nil, err
	}
	s := &deviceStore{path: p, tokens: make(map[string]deviceRecord)}
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var recs []deviceRecord
	if err := json.Unmarshal(raw, &recs); err != nil {
		return nil, err
	}
	for _, r := range recs {
		s.tokens[r.Token] = r
	}
	return s, nil
}

// Add registers token (deduping by token) and persists the updated list.
func (s *deviceStore) Add(token, platform string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("device token is empty")
	}
	s.mu.Lock()
	s.tokens[token] = deviceRecord{Token: token, Platform: platform}
	recs := make([]deviceRecord, 0, len(s.tokens))
	for _, r := range s.tokens {
		recs = append(recs, r)
	}
	path := s.path
	s.mu.Unlock()

	b, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// List returns every registered device token.
func (s *deviceStore) List() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.tokens))
	for tok := range s.tokens {
		out = append(out, tok)
	}
	return out
}

// devicesHandler handles POST /devices: {"device_token":"...","platform":"ios"}
// registers the token (200 {"ok":true}); an empty token is a 400.
func devicesHandler(store *deviceStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		var req struct {
			DeviceToken string `json:"device_token"`
			Platform    string `json:"platform"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := store.Add(req.DeviceToken, req.Platform); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

// --- APNs client ---

// apnsPusher sends alert pushes to APNs over HTTP/2 (Go's net/http
// auto-negotiates HTTP/2 for TLS connections, so no extra dependency is
// needed). httpDo is injectable for tests; now is injectable so JWT
// generation is deterministic in tests.
type apnsPusher struct {
	cfg    apnsConfig
	key    *ecdsa.PrivateKey
	httpDo func(*http.Request) (*http.Response, error)
	now    func() time.Time

	mu       sync.Mutex
	jwt      string
	jwtIssAt time.Time
}

// jwtRefresh is how long a cached JWT is reused before being re-signed.
// APNs allows tokens up to 1h old; refreshing at ~50m stays well inside that.
const jwtRefresh = 50 * time.Minute

// newAPNsPusher builds a pusher from cfg. When cfg is disabled, or the .p8
// key fails to load/parse, the returned pusher is disabled (Push is then a
// no-op returning nil) rather than erroring the caller — misconfiguration
// must never block serve startup or a turn.
func newAPNsPusher(cfg apnsConfig) *apnsPusher {
	p := &apnsPusher{
		cfg:    cfg,
		httpDo: http.DefaultClient.Do,
		now:    time.Now,
	}
	if !cfg.enabled() {
		return p
	}
	key, err := loadECPrivateKey(cfg.keyPath)
	if err != nil {
		log.Printf("apns: push disabled, failed to load key %s: %v", cfg.keyPath, err)
		return p
	}
	p.key = key
	return p
}

// enabled reports whether p can sign and send pushes. A nil pusher (or one
// whose key failed to load) is disabled.
func (p *apnsPusher) enabled() bool {
	return p != nil && p.key != nil
}

// loadECPrivateKey parses an APNs .p8 auth key: PEM-encoded PKCS#8
// containing an EC (P-256) private key.
func loadECPrivateKey(path string) (*ecdsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ec, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key in %s is not an ECDSA private key", path)
	}
	return ec, nil
}

// jwtToken returns a cached provider authentication token, re-signing it
// once jwtRefresh has elapsed since it was issued.
func (p *apnsPusher) jwtToken() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if p.jwt != "" && now.Sub(p.jwtIssAt) < jwtRefresh {
		return p.jwt, nil
	}
	tok, err := buildAPNsJWT(p.key, p.cfg.teamID, p.cfg.keyID, now)
	if err != nil {
		return "", err
	}
	p.jwt = tok
	p.jwtIssAt = now
	return tok, nil
}

// buildAPNsJWT hand-rolls an ES256 JWT (no JOSE dependency): header
// {"alg":"ES256","kid":<keyID>,"typ":"JWT"}, claims {"iss":<teamID>,
// "iat":<unix>}, signed with ECDSA over SHA-256. The signature is encoded as
// raw R||S, each padded to 32 bytes big-endian (NOT ASN.1 DER), base64url —
// the format APNs requires.
func buildAPNsJWT(key *ecdsa.PrivateKey, teamID, keyID string, now time.Time) (string, error) {
	header := struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}{Alg: "ES256", Kid: keyID, Typ: "JWT"}
	claims := struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
	}{Iss: teamID, Iat: now.Unix()}

	hb, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(cb)

	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// Push sends an alert push to deviceToken. data is merged at the top level
// of the payload (alongside "aps") for deep-linking, e.g. session_id and
// approval_id. A nil or disabled pusher is a no-op returning nil. A non-2xx
// APNs response is returned as an error, but the caller (the best-effort
// approval notifier) treats it as non-fatal.
func (p *apnsPusher) Push(deviceToken, title, body string, data map[string]string) error {
	if !p.enabled() {
		return nil
	}
	tok, err := p.jwtToken()
	if err != nil {
		return err
	}

	payload := map[string]any{
		"aps": map[string]any{
			"alert": map[string]string{"title": title, "body": body},
			"sound": "default",
		},
	}
	for k, v := range data {
		payload[k] = v
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://%s/3/device/%s", p.cfg.host(), deviceToken)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "bearer "+tok)
	req.Header.Set("apns-topic", p.cfg.bundleID)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("Content-Type", "application/json")

	do := p.httpDo
	if do == nil {
		do = http.DefaultClient.Do
	}
	resp, err := do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("apns: push failed with status %d", resp.StatusCode)
	}
	return nil
}

// --- approval-needed wiring ---

// approvalNotifier pushes an approval-needed alert to registered devices.
// Built once at serve startup; a no-op when APNs is unconfigured.
type approvalNotifier func(sessionID, approvalID, tool string)

// notifyApprovalNeeded parses the "approval-needed" SSE frame's JSON payload
// (approval_id, tool — see remoteApprovalGate) and forwards it to notify.
// Meant to be called from its own goroutine by the gate's emit wrapper, so a
// slow or failing push can never delay or error the gate. Malformed data is
// silently ignored (best-effort).
func notifyApprovalNeeded(notify approvalNotifier, sessionID, data string) {
	var payload struct {
		ApprovalID string `json:"approval_id"`
		Tool       string `json:"tool"`
	}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return
	}
	notify(sessionID, payload.ApprovalID, payload.Tool)
}

// newApprovalNotifier returns an approvalNotifier that pushes to every
// device in store via pusher. It never returns an error: individual push
// failures (and a disabled/nil pusher or store) are logged and swallowed, so
// this can be called freely from the approval gate without risk of blocking
// or erroring a turn.
func newApprovalNotifier(pusher *apnsPusher, store *deviceStore) approvalNotifier {
	return func(sessionID, approvalID, tool string) {
		if !pusher.enabled() || store == nil {
			return
		}
		data := map[string]string{"session_id": sessionID, "approval_id": approvalID}
		body := fmt.Sprintf("gophermind wants to run %s", tool)
		for _, token := range store.List() {
			if err := pusher.Push(token, "Approval needed", body, data); err != nil {
				log.Printf("apns: push to device failed: %v", err)
			}
		}
	}
}
