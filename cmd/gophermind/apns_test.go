package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- JWT ---

func TestBuildAPNsJWTHeaderAndClaims(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

	tok, err := buildAPNsJWT(key, "TEAM123", "KEYID456", now)
	if err != nil {
		t.Fatalf("buildAPNsJWT: %v", err)
	}

	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3", len(parts))
	}

	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(hb, &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if header.Alg != "ES256" {
		t.Errorf("alg = %q, want ES256", header.Alg)
	}
	if header.Kid != "KEYID456" {
		t.Errorf("kid = %q, want KEYID456", header.Kid)
	}

	cb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
	}
	if err := json.Unmarshal(cb, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.Iss != "TEAM123" {
		t.Errorf("iss = %q, want TEAM123", claims.Iss)
	}
	if claims.Iat != now.Unix() {
		t.Errorf("iat = %d, want %d", claims.Iat, now.Unix())
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if len(sig) != 64 {
		t.Fatalf("signature is %d bytes, want 64 (raw R||S)", len(sig))
	}

	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(&key.PublicKey, digest[:], r, s) {
		t.Fatal("signature does not verify against the public key")
	}
}

func TestAPNsPusherJWTCached(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	clock := now
	p := &apnsPusher{
		cfg: apnsConfig{keyID: "K", teamID: "T", bundleID: "com.example.app", env: "sandbox"},
		key: key,
		now: func() time.Time { return clock },
	}

	tok1, err := p.jwtToken()
	if err != nil {
		t.Fatalf("jwtToken: %v", err)
	}
	clock = clock.Add(10 * time.Minute)
	tok2, err := p.jwtToken()
	if err != nil {
		t.Fatalf("jwtToken: %v", err)
	}
	if tok1 != tok2 {
		t.Error("token should be cached within the 50-minute window")
	}

	clock = clock.Add(45 * time.Minute) // total 55m past issue
	tok3, err := p.jwtToken()
	if err != nil {
		t.Fatalf("jwtToken: %v", err)
	}
	if tok3 == tok1 {
		t.Error("token should be refreshed after ~50 minutes")
	}
}

// --- push request building ---

type fakeRoundTrip struct {
	req  *http.Request
	body []byte
	resp *http.Response
	err  error
}

func fakeHTTPDo(capture *fakeRoundTrip) func(*http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		capture.req = req
		if req.Body != nil {
			b, _ := io.ReadAll(req.Body)
			capture.body = b
		}
		if capture.err != nil {
			return nil, capture.err
		}
		return capture.resp, nil
	}
}

func fakeResp(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func testPusher(t *testing.T, env string, capture *fakeRoundTrip) *apnsPusher {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return &apnsPusher{
		cfg:    apnsConfig{keyID: "K", teamID: "T", bundleID: "com.example.app", env: env},
		key:    key,
		httpDo: fakeHTTPDo(capture),
		now:    func() time.Time { return time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC) },
	}
}

func TestApnsPusherPushBuildsRequestSandbox(t *testing.T) {
	capture := &fakeRoundTrip{resp: fakeResp(200)}
	p := testPusher(t, "sandbox", capture)

	err := p.Push("devtoken123", "Approval needed", "gophermind wants to run shell", map[string]string{
		"session_id":  "sess-1",
		"approval_id": "appr-1",
	})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	if capture.req == nil {
		t.Fatal("expected a captured request")
	}
	if capture.req.URL.Host != "api.sandbox.push.apple.com" {
		t.Errorf("host = %q, want api.sandbox.push.apple.com", capture.req.URL.Host)
	}
	if capture.req.URL.Path != "/3/device/devtoken123" {
		t.Errorf("path = %q, want /3/device/devtoken123", capture.req.URL.Path)
	}
	auth := capture.req.Header.Get("authorization")
	if !strings.HasPrefix(auth, "bearer ") || len(auth) < len("bearer ")+10 {
		t.Errorf("authorization header = %q, want bearer <jwt>", auth)
	}
	if got := capture.req.Header.Get("apns-topic"); got != "com.example.app" {
		t.Errorf("apns-topic = %q, want com.example.app", got)
	}
	if got := capture.req.Header.Get("apns-push-type"); got != "alert" {
		t.Errorf("apns-push-type = %q, want alert", got)
	}

	var payload struct {
		Aps struct {
			Alert struct {
				Title string `json:"title"`
				Body  string `json:"body"`
			} `json:"alert"`
			Sound string `json:"sound"`
		} `json:"aps"`
		SessionID  string `json:"session_id"`
		ApprovalID string `json:"approval_id"`
	}
	if err := json.Unmarshal(capture.body, &payload); err != nil {
		t.Fatalf("payload not JSON: %v (body=%s)", err, capture.body)
	}
	if payload.Aps.Alert.Title != "Approval needed" {
		t.Errorf("alert.title = %q", payload.Aps.Alert.Title)
	}
	if payload.Aps.Alert.Body != "gophermind wants to run shell" {
		t.Errorf("alert.body = %q", payload.Aps.Alert.Body)
	}
	if payload.SessionID != "sess-1" {
		t.Errorf("session_id = %q, want sess-1", payload.SessionID)
	}
	if payload.ApprovalID != "appr-1" {
		t.Errorf("approval_id = %q, want appr-1", payload.ApprovalID)
	}
}

func TestApnsPusherPushBuildsRequestProd(t *testing.T) {
	capture := &fakeRoundTrip{resp: fakeResp(200)}
	p := testPusher(t, "prod", capture)

	if err := p.Push("tok", "t", "b", nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if capture.req.URL.Host != "api.push.apple.com" {
		t.Errorf("host = %q, want api.push.apple.com", capture.req.URL.Host)
	}
}

func TestApnsPusherPush200ReturnsNil(t *testing.T) {
	capture := &fakeRoundTrip{resp: fakeResp(200)}
	p := testPusher(t, "sandbox", capture)
	if err := p.Push("tok", "t", "b", nil); err != nil {
		t.Errorf("Push: %v, want nil", err)
	}
}

// TestApnsPusherPushRequestHasBoundedDeadline guards against a hung APNs
// endpoint blocking the (detached) push goroutine forever: the request built
// by Push must carry a context with a deadline no more than pushTimeout out.
func TestApnsPusherPushRequestHasBoundedDeadline(t *testing.T) {
	capture := &fakeRoundTrip{resp: fakeResp(200)}
	p := testPusher(t, "sandbox", capture)

	before := time.Now()
	if err := p.Push("tok", "t", "b", nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	after := time.Now()

	deadline, ok := capture.req.Context().Deadline()
	if !ok {
		t.Fatal("request context has no deadline, want one bounded by pushTimeout")
	}
	if deadline.After(after.Add(pushTimeout)) || deadline.Before(before.Add(pushTimeout).Add(-time.Second)) {
		t.Errorf("deadline = %v, want ~%v after Push started (pushTimeout=%v)", deadline, before.Add(pushTimeout), pushTimeout)
	}
}

func TestApnsPusherPush400ReturnsError(t *testing.T) {
	capture := &fakeRoundTrip{resp: fakeResp(400)}
	p := testPusher(t, "sandbox", capture)
	if err := p.Push("tok", "t", "b", nil); err == nil {
		t.Error("Push should surface an error on 400, but caller treats it non-fatally")
	}
}

// --- device store ---

func TestDeviceStoreAddDedupeList(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	store, err := newDeviceStore()
	if err != nil {
		t.Fatalf("newDeviceStore: %v", err)
	}
	if err := store.Add("tok-a", "ios"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := store.Add("tok-b", "ios"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := store.Add("tok-a", "ios"); err != nil { // dedupe
		t.Fatalf("Add duplicate: %v", err)
	}
	list := store.List()
	if len(list) != 2 {
		t.Fatalf("List() has %d entries, want 2 (deduped): %v", len(list), list)
	}
}

func TestDeviceStorePersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)

	store, err := newDeviceStore()
	if err != nil {
		t.Fatalf("newDeviceStore: %v", err)
	}
	if err := store.Add("tok-x", "ios"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	store2, err := newDeviceStore()
	if err != nil {
		t.Fatalf("newDeviceStore (reload): %v", err)
	}
	list := store2.List()
	if len(list) != 1 || list[0] != "tok-x" {
		t.Fatalf("reloaded List() = %v, want [tok-x]", list)
	}
}

// TestDeviceStoreConcurrentAddPersistsBoth guards against the race where the
// mutex is released before the JSON marshal + file write: two goroutines
// racing Add with different tokens must both survive on disk, not just in
// memory, once the store is reloaded.
func TestDeviceStoreConcurrentAddPersistsBoth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)

	store, err := newDeviceStore()
	if err != nil {
		t.Fatalf("newDeviceStore: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- store.Add("tok-concurrent-a", "ios")
	}()
	go func() {
		defer wg.Done()
		errs <- store.Add("tok-concurrent-b", "ios")
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	reloaded, err := newDeviceStore()
	if err != nil {
		t.Fatalf("newDeviceStore (reload): %v", err)
	}
	list := reloaded.List()
	if len(list) != 2 {
		t.Fatalf("reloaded List() = %v, want 2 tokens persisted after concurrent Adds", list)
	}
	want := map[string]bool{"tok-concurrent-a": true, "tok-concurrent-b": true}
	for _, tok := range list {
		if !want[tok] {
			t.Errorf("unexpected token %q in reloaded store", tok)
		}
	}
}

func TestDeviceStorePersistFileExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPHERMIND_CONFIG_DIR", dir)
	store, err := newDeviceStore()
	if err != nil {
		t.Fatalf("newDeviceStore: %v", err)
	}
	if err := store.Add("tok-y", "ios"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*devices*"))
	if len(matches) == 0 {
		t.Error("expected a devices persistence file under the config dir")
	}
}

// --- devicesHandler ---

func TestDevicesHandlerValidAdds(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	store, err := newDeviceStore()
	if err != nil {
		t.Fatalf("newDeviceStore: %v", err)
	}
	h := devicesHandler(store)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/devices", strings.NewReader(`{"device_token":"tok-z","platform":"ios"}`))
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if !got.OK {
		t.Error("ok = false, want true")
	}
	if list := store.List(); len(list) != 1 || list[0] != "tok-z" {
		t.Errorf("List() = %v, want [tok-z]", list)
	}
}

func TestDevicesHandlerEmptyTokenBadRequest(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	store, err := newDeviceStore()
	if err != nil {
		t.Fatalf("newDeviceStore: %v", err)
	}
	h := devicesHandler(store)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/devices", strings.NewReader(`{"device_token":"","platform":"ios"}`))
	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

// --- disabled pusher / notifier ---

func TestDisabledPusherPushIsNoOp(t *testing.T) {
	var p *apnsPusher // nil pusher: fully disabled
	if err := p.Push("tok", "t", "b", nil); err != nil {
		t.Errorf("Push on nil pusher = %v, want nil", err)
	}

	p2 := &apnsPusher{cfg: apnsConfig{}} // no key configured
	if err := p2.Push("tok", "t", "b", nil); err != nil {
		t.Errorf("Push on unconfigured pusher = %v, want nil", err)
	}
}

func TestLoadAPNsConfigDisabledWhenUnset(t *testing.T) {
	t.Setenv("GOPHERMIND_APNS_KEY_P8", "")
	t.Setenv("GOPHERMIND_APNS_KEY_ID", "")
	t.Setenv("GOPHERMIND_APNS_TEAM_ID", "")
	t.Setenv("GOPHERMIND_APNS_BUNDLE_ID", "")
	cfg := loadAPNsConfig()
	if cfg.enabled() {
		t.Error("config should be disabled when key/ids are unset")
	}
	p := newAPNsPusher(cfg)
	if p.enabled() {
		t.Error("pusher built from a disabled config should be disabled")
	}
	if err := p.Push("tok", "t", "b", nil); err != nil {
		t.Errorf("disabled pusher Push = %v, want nil", err)
	}
}

func TestApprovalNotifierNoOpWhenDisabled(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	store, err := newDeviceStore()
	if err != nil {
		t.Fatalf("newDeviceStore: %v", err)
	}
	if err := store.Add("tok-1", "ios"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	var p *apnsPusher // disabled
	notify := newApprovalNotifier(p, store)
	// Must not panic and must return without attempting network I/O.
	notify("sess-1", "appr-1", "run_shell")
}

func TestApprovalNotifierPushesToAllDevices(t *testing.T) {
	t.Setenv("GOPHERMIND_CONFIG_DIR", t.TempDir())
	store, err := newDeviceStore()
	if err != nil {
		t.Fatalf("newDeviceStore: %v", err)
	}
	if err := store.Add("tok-1", "ios"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := store.Add("tok-2", "ios"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	capture := &fakeRoundTrip{resp: fakeResp(200)}
	p := testPusher(t, "sandbox", capture)
	// wrap httpDo to count calls
	calls := 0
	base := p.httpDo
	p.httpDo = func(req *http.Request) (*http.Response, error) {
		calls++
		return base(req)
	}

	notify := newApprovalNotifier(p, store)
	notify("sess-1", "appr-1", "run_shell")

	if calls != 2 {
		t.Errorf("expected a push per registered device (2), got %d calls", calls)
	}
}
