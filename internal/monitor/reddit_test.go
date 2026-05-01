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
		{"t3_4", "How can I buy tickets to The International this year in Shanghai?", "https://www.reddit.com/r/DotA2/comments/4/"},
		{"t3_5", "Anyone know when TI 2026 tickets drop?", "https://www.reddit.com/r/DotA2/comments/5/"},
		{"t3_6", "Will TI 2026 tickets be on sale soon?", "https://www.reddit.com/r/DotA2/comments/6/"},
		{"t3_7", "TI 2026 tickets discussion thread", "https://www.reddit.com/r/DotA2/comments/7/"},
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

func TestIsRedditTicketAnnouncement(t *testing.T) {
	cases := []struct {
		title string
		want  bool
	}{
		{"How can I buy tickets to The International this year in Shanghai?", false},
		{"Anyone know when TI 2026 tickets drop?", false},
		{"Will TI 2026 tickets be on sale soon?", false},
		{"Why are TI 2026 tickets on sale already? Megathread", false},
		{"TI 2026 tickets discussion thread", false},
		{"[Discussion] TI 2026 tickets on sale dates", false},
		{"Megathread: TI 2026 tickets on sale", false},
		{"TI 2026 tickets on sale rumor", false},
		{"TI 2026 tickets on sale, fake news", false},
		{"TI 2026 ticket sale joke", false},
		{"TI 2026 tickets on sale lol", false},
		{"TI 2026 tickets on sale /s", false},
		{"The International 2026 venue confirmed", false},
		{"Patch 7.41 breakdown", false},
		{"TI 2026 schedule released — tickets later this month", false},
		{"TI 2026 stream on AXS", false},
		{"TI 2026 — where to buy tickets", false},

		{"TI 2026 hopium thread — when tickets", false},
		{"What if TI 2026 tickets on sale tomorrow", false},
		{"Anyone else waiting for TI 2026 tickets on sale", false},
		{"TI 2026 tickets wishlist thread", false},

		{"TI 2026 tickets on sale — buy now on AXS!", true},
		{"The International 2026 tickets are now available", true},
		{"TI 2026 tickets just dropped on AXS", true},
		{"TI 2026 tickets dropped today", true},
		{"TI 2026 presale begins tomorrow", true},
		{"The International 2026 tickets are live on axs.com", true},
		{"TI 2026 tickets release date confirmed", true},
		{"PSA: TI 2026 tickets on sale via Valve announcement", true},
		{"TI 2026 ticket sale is live", true},
		{"TI 2026 tickets are for sale on AXS", true},
		{"TI 2026 ticket sale begins April 5", true},
		{"Get your TI 2026 tickets at axs.com", true},
		{"TI 2026 tickets went live an hour ago", true},
		{"TI 2026 tickets goes live in 2 hours", true},
		{"TI 2026 ticket info revealed", true},
		{"TI 2026 ticket prices announced", true},
		{"The International 2026 ticket details revealed", true},
		{"TI 2026 ticketing partner announced — AXS", true},
		{"TI 2026 tickets announced by Valve", true},
		{"The International 2026 tickets revealed", true},
		{"TI 2026 ticket sale date confirmed", true},
		{"TI 2026 ticket sale time leaked", true},

		{"The International 2026 tickets on sale now", true},
		{"The International 2026 presale begins tomorrow", true},
		{"The International 2026 ticketing partner announced — AXS", true},
		{"The International 2026 tickets just dropped on AXS", true},
		{"The International 2026 ticket info revealed", true},
		{"The International 2026 ticket prices announced", true},
		{"The International 2026 ticket sale begins April 5", true},
		{"Get your The International 2026 tickets at axs.com", true},

		{"Anyone know when The International 2026 tickets drop?", false},
		{"[Discussion] The International 2026 tickets on sale", false},
		{"Megathread: The International 2026 tickets on sale", false},
		{"The International 2026 hopium thread", false},
		{"How do I buy The International tickets in Shanghai?", false},

		{"TI26 tickets on sale at axs.com", true},
		{"TI 26 presale begins tomorrow", true},
		{"TI26 ticket info revealed", true},
		{"TI26 tickets dropped today", true},
		{"International 2026 tickets are live", true},
		{"International 2026 ticket sale begins April 5", true},

		{"Anyone got TI26 ticket info?", false},
		{"TI26 hopium thread", false},
		{"International 2026 schedule released", false},

		{"Tickets for The International 2026", true},
		{"The International 2026 Ticket Sales", true},
		{"The International Ticketing FAQ", true},
		{"TI 2026 ticket sale postponed to May", true},
		{"TI 2026 first wave of tickets sold out", true},
		{"TI 2026 general sale opens April 15", true},
		{"Save-the-Date: TI 2026 ticket sales", true},
		{"TI 2026 tickets sold out in minutes", true},
		{"TI 2026 ticket sales begin April 5", true},
		{"TI 2026 tickets — powered by AXS", true},
		{"TI 2026 in partnership with AXS as ticketing partner", true},
		{"TI 2026 ticket date confirmed", true},

		{"Hopefully TI 2026 tickets are on sale soon", false},
		{"I wish TI 2026 tickets dropped today", false},
		{"If only TI 2026 tickets were on sale", false},
		{"TI 2026 tickets expected to drop tomorrow", false},
		{"TI 2026 tickets rumored to go live next week", false},
		{"TI 2026 tickets gonna be on sale next month", false},
		{"TI 2026 ticket nightmare for SEA fans", false},
		{"TI 2026 ticket sale armageddon", false},
		{"TI 2026 ticket scalpers fuming", false},
		{"TI 2026 ticket woes for European fans", false},
		{"[Question] When are TI 2026 tickets on sale", false},
		{"[Help] TI 2026 tickets info", false},
		{"[Fluff] TI 2026 tickets meme", false},
		{"[Meme] TI 2026 tickets dropped today", false},
		{"Circlejerk: TI 2026 tickets on sale", false},
		{"Unpopular opinion: TI 2026 tickets too expensive", false},
		{"Hot take on TI 2026 ticket sale", false},
	}
	for _, c := range cases {
		got := isRedditTicketAnnouncement(c.title)
		if got != c.want {
			t.Errorf("isRedditTicketAnnouncement(%q) = %v, want %v", c.title, got, c.want)
		}
	}
}

func TestRedditMonitor_FetchError(t *testing.T) {
	m := &RedditMonitor{feedURL: "http://127.0.0.1:0", client: &http.Client{}}
	_, err := m.fetchAndFilter("http://127.0.0.1:0")
	if err == nil {
		t.Error("expected error on unreachable URL")
	}
}
