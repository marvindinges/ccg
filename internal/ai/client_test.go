package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marvindinges/ccg/internal/commit"
	"github.com/marvindinges/ccg/internal/config"
)

// fakeServer returns an httptest server that replies with the given assistant
// message content, and records the last request body.
func fakeServer(t *testing.T, content string, captured *chatRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			_ = json.NewDecoder(r.Body).Decode(captured)
		}
		resp := chatResponse{}
		resp.Choices = append(resp.Choices, struct {
			Message chatMessage `json:"message"`
		}{Message: chatMessage{Role: "assistant", Content: content}})
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func newClient(url string) *Client {
	return New(config.Config{Provider: config.ProviderConfig{
		BaseURL: url, Model: "test-model", APIKeyEnv: "",
	}})
}

func TestSuggestParsesCleanJSON(t *testing.T) {
	var captured chatRequest
	content := `{"type":"feat","scope":"auth","description":"add login","body":"","footers":[]}`
	srv := fakeServer(t, content, &captured)
	defer srv.Close()

	c := newClient(srv.URL)
	got, err := c.Suggest(context.Background(), SuggestInput{Diff: "diff", Types: commit.DefaultTypes()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "feat" || got.Scope != "auth" || got.Description != "add login" {
		t.Errorf("parsed commit = %+v", got)
	}
	if captured.Model != "test-model" {
		t.Errorf("request model = %q", captured.Model)
	}
	if len(captured.Messages) != 2 || captured.Messages[0].Role != "system" {
		t.Errorf("expected system+user messages, got %+v", captured.Messages)
	}
}

func TestSuggestHandlesFencedJSON(t *testing.T) {
	content := "```json\n{\"type\":\"fix\",\"description\":\"patch bug\"}\n```"
	srv := fakeServer(t, content, nil)
	defer srv.Close()

	c := newClient(srv.URL)
	got, err := c.Suggest(context.Background(), SuggestInput{Types: commit.DefaultTypes()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "fix" || got.Description != "patch bug" {
		t.Errorf("parsed = %+v", got)
	}
}

func TestSuggestHandlesPreamble(t *testing.T) {
	content := "Sure! Here is your commit:\n{\"type\":\"docs\",\"description\":\"update readme\"}\nHope that helps."
	srv := fakeServer(t, content, nil)
	defer srv.Close()

	c := newClient(srv.URL)
	got, err := c.Suggest(context.Background(), SuggestInput{Types: commit.DefaultTypes()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "docs" {
		t.Errorf("parsed = %+v", got)
	}
}

func TestSuggestBreakingFooter(t *testing.T) {
	content := `{"type":"feat","description":"new api","breaking":true,"breaking_description":"removes v1"}`
	srv := fakeServer(t, content, nil)
	defer srv.Close()

	c := newClient(srv.URL)
	got, err := c.Suggest(context.Background(), SuggestInput{Types: commit.DefaultTypes()})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Breaking {
		t.Error("expected Breaking=true")
	}
	rendered := got.Render()
	if !contains(rendered, "BREAKING CHANGE: removes v1") {
		t.Errorf("expected breaking footer in:\n%s", rendered)
	}
}

func TestSuggestErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	_, err := c.Suggest(context.Background(), SuggestInput{Types: commit.DefaultTypes()})
	if err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestStrictSchemaResponseFormat(t *testing.T) {
	c := New(config.Config{Provider: config.ProviderConfig{
		BaseURL: "http://x", Model: "m", StrictSchema: true,
	}})
	rf := c.responseFormat()
	if rf["type"] != "json_schema" {
		t.Errorf("expected json_schema, got %v", rf["type"])
	}
}

func TestResponseFormatFallback(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ResponseFormat != nil {
			// Reject the first attempt that includes response_format.
			http.Error(w, `{"error":{"message":"response_format unsupported"}}`, http.StatusBadRequest)
			return
		}
		resp := chatResponse{}
		resp.Choices = append(resp.Choices, struct {
			Message chatMessage `json:"message"`
		}{Message: chatMessage{Content: `{"type":"fix","description":"retry worked"}`}})
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	got, err := c.Suggest(context.Background(), SuggestInput{Types: commit.DefaultTypes()})
	if err != nil {
		t.Fatalf("expected fallback to succeed, got %v", err)
	}
	if got.Description != "retry worked" {
		t.Errorf("parsed = %+v", got)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts (with then without response_format), got %d", attempts)
	}
}

func TestDebugLoggerCaptures(t *testing.T) {
	content := `{"type":"feat","description":"logged"}`
	srv := fakeServer(t, content, nil)
	defer srv.Close()

	var buf bytes.Buffer
	c := newClient(srv.URL).WithLogger(log.New(&buf, "", 0))
	if _, err := c.Suggest(context.Background(), SuggestInput{Types: commit.DefaultTypes()}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"POST", "response 200", "parsed draft"} {
		if !contains(out, want) {
			t.Errorf("debug log missing %q; got:\n%s", want, out)
		}
	}
}

func TestSuggestRejectsSchemaEcho(t *testing.T) {
	// A small model echoing the JSON schema back instead of an instance: it has
	// a top-level "type":"object" and no usable description.
	content := `{"additionalProperties":false,"properties":{"description":{"type":"string"}},"required":["type","description"],"type":"object"}`
	srv := fakeServer(t, content, nil)
	defer srv.Close()

	c := newClient(srv.URL)
	_, err := c.Suggest(context.Background(), SuggestInput{Types: commit.DefaultTypes()})
	if err == nil {
		t.Fatal("expected an error when the model echoes the schema (no description)")
	}
	if !contains(err.Error(), "no usable commit") {
		t.Errorf("error should explain the problem, got: %v", err)
	}
}

func TestMaskedKey(t *testing.T) {
	withKey := New(config.Config{Provider: config.ProviderConfig{BaseURL: "x", Model: "m", APIKeyEnv: "K"}})
	t.Setenv("K", "")
	noKey := New(config.Config{Provider: config.ProviderConfig{BaseURL: "x", Model: "m", APIKeyEnv: "K"}})
	if got := noKey.maskedKey(); got == "" || got == withKey.maskedKey() {
		// noKey should report the placeholder form
		if !contains(got, "none") {
			t.Errorf("expected placeholder mask, got %q", got)
		}
	}
}

func TestBaseURLTrailingSlashTrimmed(t *testing.T) {
	c := New(config.Config{Provider: config.ProviderConfig{BaseURL: "http://x/v1/", Model: "m"}})
	if c.baseURL != "http://x/v1" {
		t.Errorf("baseURL = %q, want trailing slash trimmed", c.baseURL)
	}
}

func TestPlaceholderKeyWhenNoEnv(t *testing.T) {
	c := New(config.Config{Provider: config.ProviderConfig{BaseURL: "x", Model: "m", APIKeyEnv: ""}})
	if c.apiKey == "" {
		t.Error("expected a placeholder key for local servers")
	}
	if c.hasKey {
		t.Error("hasKey should be false when no env var configured")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
