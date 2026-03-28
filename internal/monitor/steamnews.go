package monitor

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ticketSignals = []string{
	"tickets", "ticket sale", "on sale", "presale", "pre-sale",
	"spectator pass", "viewer pass", "axs",
}

var eventSignals = []string{
	"the international", "ti 2026", "ti2026",
}

type steamNewsItem struct {
	GID      string `json:"gid"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Contents string `json:"contents"`
}

type steamNewsResponse struct {
	Appnews struct {
		NewsItems []steamNewsItem `json:"newsitems"`
	} `json:"appnews"`
}

type SteamNewsMonitor struct {
	apiURL string
	client *http.Client
}

func NewSteamNewsMonitor(apiURL string) *SteamNewsMonitor {
	return &SteamNewsMonitor{
		apiURL: apiURL,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (m *SteamNewsMonitor) Name() string { return "SteamNews" }

func (m *SteamNewsMonitor) Check() ([]Event, error) {
	items, err := m.fetch()
	if err != nil {
		return nil, err
	}
	var events []Event
	for _, item := range items {
		if isTicketNews(item.Title, item.Contents) {
			events = append(events, Event{
				ID:        item.GID,
				Title:     item.Title,
				URL:       item.URL,
				Source:    "steam",
				EventType: EventTypeAnnouncement,
			})
		}
	}
	return events, nil
}

func (m *SteamNewsMonitor) fetch() ([]steamNewsItem, error) {
	resp, err := m.client.Get(m.apiURL)
	if err != nil {
		return nil, fmt.Errorf("steam news fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steam news: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	var result steamNewsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("steam news parse: %w", err)
	}
	return result.Appnews.NewsItems, nil
}

func isTicketNews(title, contents string) bool {
	text := strings.ToLower(title + " " + contents)
	hasTicket := false
	for _, s := range ticketSignals {
		if strings.Contains(text, s) {
			hasTicket = true
			break
		}
	}
	if !hasTicket {
		return false
	}
	for _, s := range eventSignals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}
