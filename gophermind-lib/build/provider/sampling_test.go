package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"gophermind/gophermind-lib/build/provider"
)

// capture serves a fixed good reply and records every request body.
func capture(t *testing.T, cfg provider.Config) (provider.Provider, *[]map[string]any) {
	t.Helper()
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(b, &body)
		seen = append(seen, body)
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"{}"}}]}`)
	}))
	t.Cleanup(srv.Close)
	cfg.Name, cfg.BaseURL, cfg.MaxConcurrent, cfg.HTTPClient = "oss", srv.URL+"/v1", 1, srv.Client()
	cfg.Models = []provider.ModelInfo{{ID: "qwen", ContextTokens: 32768}}
	return provider.NewOpenAI(cfg), &seen
}

func TestTemperatureOverrideReplacesTheRequestsOwn(t *testing.T) {
	one := 1.0
	p, seen := capture(t, provider.Config{Temperature: &one})
	if _, err := p.Complete(context.Background(), request()); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[0]["temperature"]; got != 1.0 {
		t.Errorf("temperature = %v, want the configured 1.0 (the request asked for 0.2)", got)
	}

	p, seen = capture(t, provider.Config{})
	p.Complete(context.Background(), request())
	if got := (*seen)[0]["temperature"]; got != 0.2 {
		t.Errorf("temperature = %v, want the request's own 0.2 when none is configured", got)
	}
}

func TestZeroTemperatureOverrideIsHonoured(t *testing.T) {
	zero := 0.0
	p, seen := capture(t, provider.Config{Temperature: &zero})
	r := request()
	r.Temperature = 0.7
	p.Complete(context.Background(), r)
	if got := (*seen)[0]["temperature"]; got != 0.0 {
		t.Errorf("temperature = %v, want an explicit 0 to win over the request's 0.7", got)
	}
}

func TestJSONModeAsksForJSONOnlyWhenBothSidesWantIt(t *testing.T) {
	cases := []struct {
		name     string
		jsonMode bool
		reqJSON  bool
		want     bool
	}{
		{"provider on, call expects JSON", true, true, true},
		{"provider on, call is free text", true, false, false},
		{"provider off, call expects JSON", false, true, false},
	}
	for _, c := range cases {
		p, seen := capture(t, provider.Config{JSONMode: c.jsonMode})
		r := request()
		r.JSON = c.reqJSON
		if _, err := p.Complete(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		rf, has := (*seen)[0]["response_format"].(map[string]any)
		if has != c.want || (has && rf["type"] != "json_object") {
			t.Errorf("%s: response_format = %v, want sent=%v as json_object", c.name, (*seen)[0]["response_format"], c.want)
		}
	}
}

// A call that knows the exact shape of its reply passes a schema, and a provider
// in json_mode has its server enforce it (not only that the text is JSON): a
// model once wrote a list of objects as {"}, then a string holding the rest.
func TestJSONModeSendsTheCallsSchemaWhenItHasOne(t *testing.T) {
	p, seen := capture(t, provider.Config{JSONMode: true})
	r := request()
	r.JSON = true
	r.Schema = []byte(`{"type":"object","required":["tests"]}`)
	if _, err := p.Complete(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	rf, _ := (*seen)[0]["response_format"].(map[string]any)
	if rf["type"] != "json_schema" {
		t.Fatalf("response_format = %v, want json_schema", (*seen)[0]["response_format"])
	}
	js, _ := rf["json_schema"].(map[string]any)
	sch, _ := js["schema"].(map[string]any)
	if sch["type"] != "object" || js["name"] == "" {
		t.Errorf("json_schema = %v", js)
	}

	p, seen = capture(t, provider.Config{JSONMode: true})
	r.Schema = nil
	p.Complete(context.Background(), r)
	if rf, _ := (*seen)[0]["response_format"].(map[string]any); rf["type"] != "json_object" {
		t.Errorf("without a schema the format = %v, want json_object", (*seen)[0]["response_format"])
	}

	p, seen = capture(t, provider.Config{})
	r.Schema = []byte(`{"type":"object"}`)
	p.Complete(context.Background(), r)
	if _, has := (*seen)[0]["response_format"]; has {
		t.Error("a provider not in json_mode was sent a response_format")
	}
}
