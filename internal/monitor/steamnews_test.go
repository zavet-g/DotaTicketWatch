package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsTicketNews(t *testing.T) {
	cases := []struct {
		title    string
		contents string
		want     bool
	}{
		{"Tickets for The International 2026 now on sale", "", true},
		{"Buy your dota tickets here", "limited availability", true},
		{"The International 2026 tickets are now on sale", "", true},
		{"Dota 2 ticket sales announced for The International", "", true},
		{"Patch 7.41 released", "hero balance changes", false},
		{"Dota 2 Update", "new cosmetics added", false},
		{"The International 2026 — Group Stage Schedule", "", false},
		{"The International 2026", "venue announced, Shanghai", false},
		{"Summer Sale on Steam", "games on sale now", false},
	}
	for _, c := range cases {
		got := isTicketNews(c.title, c.contents)
		if got != c.want {
			t.Errorf("isTicketNews(%q, %q) = %v, want %v", c.title, c.contents, got, c.want)
		}
	}
}

func TestSteamNewsMonitor_Check_ReturnsSameEventsEveryCall(t *testing.T) {
	payload := map[string]any{
		"appnews": map[string]any{
			"newsitems": []map[string]any{
				{"gid": "100", "title": "Dota 2 tickets for The International 2026!", "url": "https://dota2.com/news/100", "contents": ""},
				{"gid": "101", "title": "Patch notes 7.41", "url": "https://dota2.com/news/101", "contents": "balance"},
			},
		},
	}
	body, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	m := NewSteamNewsMonitor(srv.URL)

	for call := 1; call <= 3; call++ {
		events, err := m.Check()
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", call, err)
		}
		if len(events) != 1 {
			t.Fatalf("call %d: expected 1 ticket event, got %d (monitor must be stateless)", call, len(events))
		}
		if events[0].ID != "100" {
			t.Errorf("call %d: expected event ID 100, got %s", call, events[0].ID)
		}
	}
}
