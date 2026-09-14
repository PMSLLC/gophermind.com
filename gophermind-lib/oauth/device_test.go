package oauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeviceFlow(t *testing.T) {
	polls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/device", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"device_code":"DC","user_code":"WXYZ","verification_uri":"http://verify","interval":1}`))
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		polls++
		if polls < 2 {
			w.Write([]byte(`{"error":"authorization_pending"}`))
			return
		}
		w.Write([]byte(`{"access_token":"TOKEN123"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := Config{ClientID: "cid", DeviceURL: srv.URL + "/device", TokenURL: srv.URL + "/token", Sleep: func(time.Duration) {}}
	dc, err := cfg.RequestDeviceCode()
	if err != nil {
		t.Fatal(err)
	}
	if dc.UserCode != "WXYZ" || dc.DeviceCode != "DC" {
		t.Fatalf("device code wrong: %+v", dc)
	}
	token, err := cfg.PollToken(dc, 5)
	if err != nil {
		t.Fatal(err)
	}
	if token != "TOKEN123" {
		t.Errorf("token = %q, want TOKEN123", token)
	}
	if polls != 2 {
		t.Errorf("expected 2 polls (pending then success), got %d", polls)
	}
}

func TestDeviceFlowError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error":"access_denied"}`))
	}))
	defer srv.Close()
	cfg := Config{ClientID: "c", TokenURL: srv.URL, Sleep: func(time.Duration) {}}
	if _, err := cfg.PollToken(&DeviceCode{DeviceCode: "x", Interval: 1}, 3); err == nil {
		t.Error("access_denied should abort the flow")
	}
}
