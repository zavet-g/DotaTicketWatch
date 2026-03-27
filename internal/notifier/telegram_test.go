package notifier

import (
	"strings"
	"testing"

	"github.com/artem/dotaticketwatch/internal/monitor"
)

func TestFormatEvent_AllSources(t *testing.T) {
	cases := []struct {
		event       monitor.Event
		wantContain []string
	}{
		{
			event: monitor.Event{
				Source: "axs",
				Title:  "The International 2026",
				URL:    "https://www.axs.com/events/123/ti",
			},
			wantContain: []string{"билеты на TI 2026", "купить на AXS", "axs.com/events/123"},
		},
		{
			event: monitor.Event{
				Source: "steam",
				Title:  "TI 2026 Ticket Sale Announced",
				URL:    "https://store.steampowered.com/news/app/570",
			},
			wantContain: []string{"анонс", "Valve", "читать"},
		},
		{
			event: monitor.Event{
				Source: "reddit",
				Title:  "TI 2026 tickets on sale now!",
				URL:    "https://www.reddit.com/r/DotA2/comments/abc123/",
			},
			wantContain: []string{"r/DotA2", "reddit.com/r/DotA2"},
		},
	}

	for _, c := range cases {
		text := formatEvent(c.event)
		for _, want := range c.wantContain {
			if !strings.Contains(text, want) {
				t.Errorf("source=%q: formatEvent missing %q\ngot: %s", c.event.Source, want, text)
			}
		}
		if !strings.Contains(text, "🚨") {
			t.Errorf("source=%q: missing 🚨", c.event.Source)
		}
	}
}

func TestFormatEvent_UnknownSource(t *testing.T) {
	e := monitor.Event{Source: "unknown", Title: "some title", URL: "https://example.com"}
	text := formatEvent(e)
	if !strings.Contains(text, "some title") {
		t.Errorf("default case must include title, got: %s", text)
	}
}
