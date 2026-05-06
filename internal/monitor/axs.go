package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/artem/dotaticketwatch/internal/ai"
)

type axsStore interface {
	AIStateGet(key string) ([]byte, bool)
	AIStateSet(key string, value []byte) error
}

const axsSnapshotKey = "axs_snapshot"

var axsNextDataRegex = regexp.MustCompile(`<script id="__NEXT_DATA__" type="application/json">([\s\S]*?)</script>`)
var axsEventURLRegex = regexp.MustCompile(`/events/(\d{5,8})/`)

var axsQueueItPatterns = []string{
	"queueit-overlay",
	"queueittoken=",
	"inqueue.queue-it.net",
	"estimated waiting time",
	"you are currently in line",
}

type axsNextData struct {
	Props struct {
		PageProps struct {
			PerformerEventsData struct {
				TotalEvents int            `json:"totalEvents"`
				EventItems  []axsEventItem `json:"eventItems"`
			} `json:"performerEventsData"`
			TeamUpcomingEventData struct {
				HasUpcomingEvent bool         `json:"hasUpcomingEvent"`
				UpcomingEvent    axsEventItem `json:"upcomingEvent"`
			} `json:"teamUpcomingEventData"`
			DiscoveryPerformerData struct {
				Events []axsEventItem `json:"events"`
			} `json:"discoveryPerformerData"`
		} `json:"pageProps"`
	} `json:"props"`
}

type axsMedia struct {
	FileName    string `json:"fileName"`
	MediaHref   string `json:"mediaHref"`
	MediaTypeID int    `json:"mediaTypeId"`
}

func (m axsMedia) imageURL() string {
	if m.FileName != "" {
		return m.FileName
	}
	return m.MediaHref
}

type axsEventItem struct {
	ID           json.Number `json:"id"`
	EventName    string      `json:"eventName"`
	URLSlug      string      `json:"urlSlug"`
	Date         string      `json:"date"`
	VenueCity    string      `json:"venueCity"`
	VenueTitle   string      `json:"venueTitle"`
	StatusID     int         `json:"statusId"`
	Media        []axsMedia  `json:"media"`
	RelatedMedia []axsMedia  `json:"relatedMedia"`
}

type AXSMonitor struct {
	hubURL          string
	flareSolverrURL string
	fetchFn         func(url, flareSolverrURL string) (string, error)
	aiClient        ai.Client
	store           axsStore
	diffEnabled     bool
	adminFn         func(string)
}

func NewAXSMonitor(hubURL, flareSolverrURL string, fetchFn func(string, string) (string, error), aiClient ai.Client, store axsStore, diffEnabled bool, adminFn func(string)) *AXSMonitor {
	return &AXSMonitor{
		hubURL:          hubURL,
		flareSolverrURL: flareSolverrURL,
		fetchFn:         fetchFn,
		aiClient:        aiClient,
		store:           store,
		diffEnabled:     diffEnabled,
		adminFn:         adminFn,
	}
}

func (m *AXSMonitor) Name() string { return "AXS" }

func (m *AXSMonitor) HubURL() string { return m.hubURL }

func (m *AXSMonitor) FetchHTML() (string, error) {
	return m.fetchFn(m.hubURL, m.flareSolverrURL)
}

func (m *AXSMonitor) Check() ([]Event, error) {
	html, err := m.fetchFn(m.hubURL, m.flareSolverrURL)
	if err != nil {
		return nil, fmt.Errorf("axs fetch: %w", err)
	}
	events, parseErr := extractAXSEvents(html)

	if m.diffEnabled && m.aiClient != nil && m.aiClient.IsEnabled() && m.store != nil {
		m.runDiff(html)
	}

	if parseErr == nil && len(events) > 0 {
		return events, nil
	}
	if m.aiClient != nil && m.aiClient.IsEnabled() {
		aiEvents, aiErr := ParseAXSWithAI(context.Background(), m.aiClient, html)
		if aiErr == nil && len(aiEvents) > 0 {
			slog.Info("axs ai-fallback hit", "count", len(aiEvents))
			return aiEvents, nil
		}
	}
	return events, parseErr
}

func (m *AXSMonitor) runDiff(html string) {
	pruned, err := pruneAXSSnapshot(html)
	if err != nil {
		return
	}
	prev, ok := m.store.AIStateGet(axsSnapshotKey)
	_ = m.store.AIStateSet(axsSnapshotKey, pruned)
	if !ok {
		return
	}
	if bytes.Equal(prev, pruned) {
		return
	}
	signal, err := AnalyzeAXSDiff(context.Background(), m.aiClient, string(prev), string(pruned))
	if err != nil {
		slog.Warn("axs diff ai failed", "err", err)
		return
	}
	if signal.IsPreSaleSignal && signal.Confidence >= 0.7 && m.adminFn != nil {
		var sb strings.Builder
		sb.WriteString("▸ <b>AXS diff</b> · возможный сигнал\n")
		sb.WriteString(fmt.Sprintf("<i>%s</i>\n", signal.Summary))
		for _, ch := range signal.Changes {
			sb.WriteString(fmt.Sprintf("• %s\n", ch))
		}
		sb.WriteString(fmt.Sprintf("<code>confidence %.2f</code>", signal.Confidence))
		m.adminFn(sb.String())
	}
}

func pruneAXSSnapshot(html string) ([]byte, error) {
	nd, err := parseNextData(html)
	if err != nil || nd == nil {
		return nil, fmt.Errorf("no __NEXT_DATA__")
	}
	pruned := struct {
		PerformerEvents any `json:"performerEvents"`
		TeamUpcoming    any `json:"teamUpcoming"`
		Discovery       any `json:"discovery"`
	}{
		PerformerEvents: nd.Props.PageProps.PerformerEventsData,
		TeamUpcoming:    nd.Props.PageProps.TeamUpcomingEventData,
		Discovery:       nd.Props.PageProps.DiscoveryPerformerData,
	}
	return json.Marshal(pruned)
}

func looksLikeAXS(html string) bool {
	lower := strings.ToLower(html)
	return strings.Contains(lower, "axs.com") ||
		strings.Contains(lower, "__next_data__") ||
		strings.Contains(lower, "axs-event") ||
		strings.Contains(lower, "performereventsdata")
}

func extractAXSEvents(html string) ([]Event, error) {
	if !looksLikeAXS(html) {
		return nil, fmt.Errorf("axs page anomaly: HTML not from axs.com")
	}

	seen := make(map[string]bool)
	var events []Event

	nd, ndErr := parseNextData(html)
	if ndErr != nil {
		return nil, ndErr
	}
	if nd != nil {
		pp := nd.Props.PageProps

		for _, item := range pp.PerformerEventsData.EventItems {
			if e, ok := itemToEvent(item); ok && !seen[e.ID] {
				seen[e.ID] = true
				events = append(events, e)
			}
		}

		if pp.TeamUpcomingEventData.HasUpcomingEvent {
			if e, ok := itemToEvent(pp.TeamUpcomingEventData.UpcomingEvent); ok && !seen[e.ID] {
				seen[e.ID] = true
				events = append(events, e)
			}
		}

		for _, item := range pp.DiscoveryPerformerData.Events {
			if e, ok := itemToEvent(item); ok && !seen[e.ID] {
				seen[e.ID] = true
				events = append(events, e)
			}
		}
	} else {
		if isQueueItActive(html) {
			events = append(events, Event{
				ID:        "axs-queueit-active",
				Title:     "очередь Queue-it активна · возможно, билеты в продаже",
				URL:       "https://www.axs.com/teams/1119906/the-international-dota-2-tickets",
				Source:    "axs",
				EventType: EventTypeSale,
			})
			return events, nil
		}
		if len(extractIDsFromHTML(html)) == 0 {
			return nil, fmt.Errorf("axs page anomaly: no __NEXT_DATA__, no queue-it, no event IDs")
		}
	}

	for _, id := range extractIDsFromHTML(html) {
		if !seen[id] {
			seen[id] = true
			events = append(events, Event{
				ID:        id,
				Title:     "The International 2026 — билеты в продаже",
				URL:       fmt.Sprintf("https://www.axs.com/events/%s/the-international-dota-2-tickets", id),
				Source:    "axs",
				EventType: EventTypeSale,
			})
		}
	}

	return events, nil
}

func parseNextData(html string) (*axsNextData, error) {
	m := axsNextDataRegex.FindStringSubmatch(html)
	if m == nil {
		return nil, nil
	}
	var nd axsNextData
	if err := json.Unmarshal([]byte(m[1]), &nd); err != nil {
		return nil, fmt.Errorf("__NEXT_DATA__ found but JSON broken: %w", err)
	}
	return &nd, nil
}

func isQueueItActive(html string) bool {
	lower := strings.ToLower(html)
	for _, pattern := range axsQueueItPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func itemToEvent(item axsEventItem) (Event, bool) {
	idStr := item.ID.String()
	if idStr == "" || idStr == "0" {
		return Event{}, false
	}
	if n, err := strconv.ParseInt(idStr, 10, 64); err != nil || n == 0 {
		return Event{}, false
	}

	slug := item.URLSlug
	if slug == "" {
		slug = "the-international-dota-2-tickets"
	}
	title := item.EventName
	if title == "" {
		title = "The International 2026 — билеты в продаже"
	}
	location := ""
	if item.VenueCity != "" {
		location = fmt.Sprintf("📍 %s", item.VenueCity)
	}
	if item.Date != "" && location != "" {
		location += " · " + item.Date
	}
	if location != "" {
		title = title + "\n" + location
	}

	media := item.Media
	if len(media) == 0 {
		media = item.RelatedMedia
	}
	return Event{
		ID:        idStr,
		Title:     title,
		URL:       fmt.Sprintf("https://www.axs.com/events/%s/%s", idStr, slug),
		Source:    "axs",
		EventType: EventTypeSale,
		ImageURL:  pickBestImage(media),
	}, true
}

func pickBestImage(media []axsMedia) string {
	for _, wantID := range []int{17, 18, 1} {
		for _, m := range media {
			if m.MediaTypeID == wantID {
				if u := m.imageURL(); u != "" && !isPlaceholder(u) {
					return u
				}
			}
		}
	}
	for _, m := range media {
		if u := m.imageURL(); u != "" && !isPlaceholder(u) {
			return u
		}
	}
	return ""
}

func isPlaceholder(url string) bool {
	return strings.Contains(url, "/axs/bundles/aegaxs/images/defaults/")
}

func extractIDsFromHTML(html string) []string {
	matches := axsEventURLRegex.FindAllStringSubmatch(html, -1)
	seen := make(map[string]bool)
	var ids []string
	for _, m := range matches {
		id := m[1]
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

