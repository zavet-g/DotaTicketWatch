package monitor

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ticketPhrases = [][]string{
	{"ticket", "international"},
	{"ticket", "dota"},
	{"on sale", "international"},
	{"buy", "ticket"},
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
				ID:     item.GID,
				Title:  item.Title,
				URL:    item.URL,
				Source: "steam",
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
	body, err := io.ReadAll(resp.Body)
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
	for _, phrase := range ticketPhrases {
		if allContained(text, phrase) {
			return true
		}
	}
	return false
}

func allContained(text string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}
