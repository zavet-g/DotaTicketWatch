package monitor

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func makeAtomFeed(entries []struct{ id, title, href string }) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<feed xmlns="http://www.w3.org/2005/Atom">`)
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf(
			`<entry><id>%s</id><title>%s</title><link href="%s"/></entry>`,
			e.id, e.title, e.href,
		))
	}
	sb.WriteString(`</feed>`)
	return sb.String()
}

func TestRedditMonitor_EventFields(t *testing.T) {
	feed := makeAtomFeed([]struct{ id, title, href string }{
		{"t3_abc123", "TI 2026 tickets on sale — buy now on AXS!", "https://www.reddit.com/r/DotA2/comments/abc123/ti_tickets/"},
		{"t3_xyz999", "New hero teaser", "https://www.reddit.com/r/DotA2/comments/xyz999/hero/"},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprint(w, feed)
	}))
	defer srv.Close()

	m := &RedditMonitor{feedURL: srv.URL, client: srv.Client()}
	events, err := m.fetchAndFilter(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Source != "reddit" {
		t.Errorf("Source = %q, want %q", e.Source, "reddit")
	}
	if e.EventType != EventTypeAnnouncement {
		t.Errorf("EventType = %q, want %q", e.EventType, EventTypeAnnouncement)
	}
	if e.ID != "t3_abc123" {
		t.Errorf("ID = %q, want t3_abc123", e.ID)
	}
	if !strings.Contains(e.URL, "reddit.com") {
		t.Errorf("URL = %q, expected reddit.com", e.URL)
	}
}

func TestRedditMonitor_NoFalsePositives(t *testing.T) {
	feed := makeAtomFeed([]struct{ id, title, href string }{
		{"t3_1", "Patch 7.41 breakdown", "https://www.reddit.com/r/DotA2/comments/1/"},
		{"t3_2", "The International 2026 venue confirmed", "https://www.reddit.com/r/DotA2/comments/2/"},
		{"t3_3", "Summer Sale on Steam — games on sale", "https://www.reddit.com/r/DotA2/comments/3/"},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, feed)
	}))
	defer srv.Close()

	m := &RedditMonitor{feedURL: srv.URL, client: srv.Client()}
	events, err := m.fetchAndFilter(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d: %+v", len(events), events)
	}
}

func TestRedditMonitor_FetchError(t *testing.T) {
	m := &RedditMonitor{feedURL: "http://127.0.0.1:0", client: &http.Client{}}
	_, err := m.fetchAndFilter("http://127.0.0.1:0")
	if err == nil {
		t.Error("expected error on unreachable URL")
	}
}
