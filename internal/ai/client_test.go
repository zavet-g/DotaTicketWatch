package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNoopClientReturnsErrAINotEnabled(t *testing.T) {
	c := NewClient(Config{}, nil)
	if c.IsEnabled() {
		t.Fatal("noop should not be enabled")
	}
	_, err := c.Complete(context.Background(), CompleteRequest{User: "test"})
	if err != ErrAINotEnabled {
		t.Fatalf("want ErrAINotEnabled, got %v", err)
	}
}

func TestHashKeyStable(t *testing.T) {
	a := HashKey("a", "b", "c")
	b := HashKey("a", "b", "c")
	if a != b {
		t.Fatalf("hash unstable: %s vs %s", a, b)
	}
	c := HashKey("a", "b", "d")
	if a == c {
		t.Fatalf("hash collision: %s == %s", a, c)
	}
}

type memCache struct {
	data map[string][]byte
}

func (m *memCache) Get(key string) ([]byte, bool) {
	v, ok := m.data[key]
	return v, ok
}

func (m *memCache) Set(key string, value []byte, _ time.Duration) error {
	if m.data == nil {
		m.data = map[string][]byte{}
	}
	m.data[key] = value
	return nil
}

func TestOpenAIClientHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing auth header")
		}
		_ = json.NewEncoder(w).Encode(oaiResponse{
			Choices: []oaiChoice{{Message: oaiMessage{Role: "assistant", Content: "hello"}}},
			Usage:   oaiUsage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
		})
	}))
	defer srv.Close()

	cache := &memCache{}
	c := NewClient(Config{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	}, cache)

	if !c.IsEnabled() {
		t.Fatal("client should be enabled")
	}

	resp, err := c.Complete(context.Background(), CompleteRequest{
		System: "sys",
		User:   "hi",
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("content: %q", resp.Content)
	}
	if resp.Usage.TotalTokens != 12 {
		t.Errorf("tokens: %d", resp.Usage.TotalTokens)
	}
	if resp.Usage.Cached {
		t.Errorf("first call should not be cached")
	}

	resp2, err := c.Complete(context.Background(), CompleteRequest{
		System: "sys",
		User:   "hi",
	})
	if err != nil {
		t.Fatalf("complete2: %v", err)
	}
	if !resp2.Usage.Cached {
		t.Errorf("second call should be cached")
	}
}

func TestOpenAIClientUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	c := NewClient(Config{APIKey: "bad", BaseURL: srv.URL, Timeout: 2 * time.Second}, nil)
	_, err := c.Complete(context.Background(), CompleteRequest{User: "x"})
	if err != ErrUnauthorized {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestOpenAIClientAlertsOnceUntilRecovery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(502)
	}))
	defer srv.Close()

	var alerts int
	c := NewClient(Config{
		APIKey:       "k",
		BaseURL:      srv.URL,
		Timeout:      2 * time.Second,
		OnFailureRun: func(error) { alerts++ },
	}, nil).(*openaiClient)

	for i := 0; i < consecutiveFailuresLimit; i++ {
		_, _ = c.Complete(context.Background(), CompleteRequest{User: "x"})
	}
	if alerts != 1 {
		t.Fatalf("first disable: want 1 alert, got %d", alerts)
	}

	c.disabledAtNanos.Store(time.Now().Add(-7 * time.Hour).UnixNano())
	if !c.IsEnabled() {
		t.Fatal("cooldown should have passed")
	}
	for i := 0; i < consecutiveFailuresLimit; i++ {
		_, _ = c.Complete(context.Background(), CompleteRequest{User: "x"})
	}
	if alerts != 1 {
		t.Fatalf("re-disable without recovery must not re-alert, got %d", alerts)
	}
}

func TestOpenAIClientDisablesAfterFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := NewClient(Config{APIKey: "k", BaseURL: srv.URL, Timeout: 2 * time.Second}, nil)
	for i := 0; i < consecutiveFailuresLimit; i++ {
		_, _ = c.Complete(context.Background(), CompleteRequest{User: "x"})
	}
	if c.IsEnabled() {
		t.Fatal("client should be disabled after failures")
	}
	_, err := c.Complete(context.Background(), CompleteRequest{User: "x"})
	if err != ErrAINotEnabled {
		t.Fatalf("want ErrAINotEnabled after disable, got %v", err)
	}
}
