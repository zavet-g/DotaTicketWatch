package monitor

import (
	"encoding/json"
	"fmt"
	"testing"
)

func makeNextDataHTML(items []axsEventItem, upcoming *axsEventItem, discovery []axsEventItem) string {
	data := axsNextData{}
	data.Props.PageProps.PerformerEventsData.EventItems = items
	if upcoming != nil {
		data.Props.PageProps.TeamUpcomingEventData.HasUpcomingEvent = true
		data.Props.PageProps.TeamUpcomingEventData.UpcomingEvent = *upcoming
	}
	if discovery != nil {
		data.Props.PageProps.DiscoveryPerformerData.Events = discovery
	}
	b, _ := json.Marshal(data)
	return fmt.Sprintf(`<script id="__NEXT_DATA__" type="application/json">%s</script>`, b)
}

func TestParseNextData_Valid(t *testing.T) {
	item := axsEventItem{ID: json.Number("916200"), EventName: "The International 2026"}
	html := makeNextDataHTML([]axsEventItem{item}, nil, nil)

	nd := parseNextData(html)
	if nd == nil {
		t.Fatal("expected non-nil result")
	}
	items := nd.Props.PageProps.PerformerEventsData.EventItems
	if len(items) != 1 || items[0].EventName != "The International 2026" {
		t.Errorf("unexpected items: %+v", items)
	}
}

func TestParseNextData_NoScriptTag(t *testing.T) {
	if parseNextData("<html><body>nothing</body></html>") != nil {
		t.Error("expected nil when no __NEXT_DATA__ script tag")
	}
}

func TestParseNextData_InvalidJSON(t *testing.T) {
	html := `<script id="__NEXT_DATA__" type="application/json">{bad json}</script>`
	if parseNextData(html) != nil {
		t.Error("expected nil for invalid JSON")
	}
}

func TestItemToEvent_Valid(t *testing.T) {
	item := axsEventItem{
		ID:        json.Number("916200"),
		EventName: "The International 2026",
		URLSlug:   "the-international-dota-2-tickets",
		VenueCity: "Hamburg",
		Date:      "2026-09-11",
	}
	e, ok := itemToEvent(item)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if e.ID != "916200" {
		t.Errorf("expected ID 916200, got %s", e.ID)
	}
	if e.Source != "axs" {
		t.Errorf("expected source axs, got %s", e.Source)
	}
	if e.EventType != EventTypeSale {
		t.Errorf("EventType = %q, want %q", e.EventType, EventTypeSale)
	}
	if e.URL == "" {
		t.Error("expected non-empty URL")
	}
}

func TestItemToEvent_ZeroID(t *testing.T) {
	_, ok := itemToEvent(axsEventItem{ID: json.Number("0")})
	if ok {
		t.Error("expected ok=false for zero ID")
	}
}

func TestItemToEvent_EmptyID(t *testing.T) {
	_, ok := itemToEvent(axsEventItem{ID: json.Number("")})
	if ok {
		t.Error("expected ok=false for empty ID")
	}
}

func TestExtractAXSEvents_FromEventItems(t *testing.T) {
	item := axsEventItem{ID: json.Number("916200"), URLSlug: "the-international-dota-2-tickets"}
	events, err := extractAXSEvents(makeNextDataHTML([]axsEventItem{item}, nil, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 || events[0].ID != "916200" {
		t.Errorf("expected 1 event with ID 916200, got %+v", events)
	}
}

func TestExtractAXSEvents_FromUpcomingEvent(t *testing.T) {
	upcoming := axsEventItem{ID: json.Number("916201"), URLSlug: "ti-2026"}
	events, err := extractAXSEvents(makeNextDataHTML(nil, &upcoming, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 || events[0].ID != "916201" {
		t.Errorf("expected 1 event with ID 916201, got %+v", events)
	}
}

func TestExtractAXSEvents_FromDiscoveryData(t *testing.T) {
	discovery := []axsEventItem{{ID: json.Number("916202"), URLSlug: "ti-2026"}}
	events, err := extractAXSEvents(makeNextDataHTML(nil, nil, discovery))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 || events[0].ID != "916202" {
		t.Errorf("expected 1 event from discoveryPerformerData, got %+v", events)
	}
}

func TestExtractAXSEvents_NoDuplicatesAcrossSources(t *testing.T) {
	item := axsEventItem{ID: json.Number("916200"), URLSlug: "ti-2026"}
	events, err := extractAXSEvents(makeNextDataHTML(
		[]axsEventItem{item},
		&item,
		[]axsEventItem{item},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event (no duplicates across sources), got %d", len(events))
	}
}

func TestExtractAXSEvents_EmptyPage(t *testing.T) {
	events, err := extractAXSEvents("<html><body>No events yet</body></html>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestExtractAXSEvents_QueueItActive(t *testing.T) {
	html := `<html><body>
		<div id="queueit-overlay">You are currently in line</div>
	</body></html>`
	events, err := extractAXSEvents(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 alert event for active Queue-it, got %d", len(events))
	}
	if events[0].ID != "axs-queueit-active" {
		t.Errorf("unexpected event ID: %s", events[0].ID)
	}
	if events[0].Source != "axs" {
		t.Errorf("unexpected source: %s", events[0].Source)
	}
}

func TestIsQueueItActive_Inactive(t *testing.T) {
	html := `<script src="https://static.queue-it.net/script/queueclient.min.js"></script>`
	if isQueueItActive(html) {
		t.Error("expected false: Queue-it script present but waitroom not active")
	}
}

func TestIsQueueItActive_Active(t *testing.T) {
	cases := []string{
		`<div id="queueit-overlay">waiting</div>`,
		`https://inqueue.queue-it.net/inqueue/waiting`,
		`<p>Estimated waiting time: 5 minutes</p>`,
		`?queueittoken=abc123`,
	}
	for _, html := range cases {
		if !isQueueItActive(html) {
			t.Errorf("expected true for: %s", html[:50])
		}
	}
}

func TestAXSMonitor_Check_StatelessReturnsEveryCall(t *testing.T) {
	item := axsEventItem{ID: json.Number("916200"), URLSlug: "the-international-dota-2-tickets"}
	html := makeNextDataHTML([]axsEventItem{item}, nil, nil)

	mockFetch := func(url, _ string) (string, error) { return html, nil }
	m := NewAXSMonitor("https://axs.com/hub", "", mockFetch)

	for call := 1; call <= 3; call++ {
		events, err := m.Check()
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", call, err)
		}
		if len(events) != 1 {
			t.Fatalf("call %d: expected 1 event, got %d (monitor must be stateless)", call, len(events))
		}
		if events[0].ID != "916200" {
			t.Errorf("call %d: expected ID 916200, got %s", call, events[0].ID)
		}
	}
}

func TestAXSMonitor_Check_FetchError(t *testing.T) {
	mockFetch := func(url, _ string) (string, error) {
		return "", fmt.Errorf("cloudflare blocked")
	}
	m := NewAXSMonitor("https://axs.com/hub", "", mockFetch)
	_, err := m.Check()
	if err == nil {
		t.Error("expected error when fetch fails, got nil")
	}
}

func TestAXSMonitor_Check_NoEvents(t *testing.T) {
	mockFetch := func(url, _ string) (string, error) {
		return "<html><body>No events yet</body></html>", nil
	}
	m := NewAXSMonitor("https://axs.com/hub", "", mockFetch)
	events, err := m.Check()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}
