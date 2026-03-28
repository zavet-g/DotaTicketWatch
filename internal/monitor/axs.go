package monitor

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

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
}

func NewAXSMonitor(hubURL, flareSolverrURL string, fetchFn func(string, string) (string, error)) *AXSMonitor {
	return &AXSMonitor{
		hubURL:          hubURL,
		flareSolverrURL: flareSolverrURL,
		fetchFn:         fetchFn,
	}
}

func (m *AXSMonitor) Name() string { return "AXS" }

func (m *AXSMonitor) Check() ([]Event, error) {
	html, err := m.fetchFn(m.hubURL, m.flareSolverrURL)
	if err != nil {
		return nil, fmt.Errorf("axs fetch: %w", err)
	}
	return extractAXSEvents(html)
}

func extractAXSEvents(html string) ([]Event, error) {
	seen := make(map[string]bool)
	var events []Event

	if nd := parseNextData(html); nd != nil {
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

func parseNextData(html string) *axsNextData {
	m := axsNextDataRegex.FindStringSubmatch(html)
	if m == nil {
		return nil
	}
	var nd axsNextData
	if err := json.Unmarshal([]byte(m[1]), &nd); err != nil {
		return nil
	}
	return &nd
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

