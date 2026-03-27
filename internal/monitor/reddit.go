package monitor

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"time"
)

type atomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Entries []atomEntry `xml:"http://www.w3.org/2005/Atom entry"`
}

type atomEntry struct {
	ID    string   `xml:"http://www.w3.org/2005/Atom id"`
	Title string   `xml:"http://www.w3.org/2005/Atom title"`
	Link  atomLink `xml:"http://www.w3.org/2005/Atom link"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
}

type RedditMonitor struct {
	feedURL string
	client  *http.Client
}

func NewRedditMonitor() *RedditMonitor {
	return &RedditMonitor{
		feedURL: "https://www.reddit.com/r/DotA2/new.rss",
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (m *RedditMonitor) Name() string { return "Reddit/r/DotA2" }

func (m *RedditMonitor) Check() ([]Event, error) {
	return m.fetchAndFilter(m.feedURL)
}

func (m *RedditMonitor) fetchAndFilter(url string) ([]Event, error) {
	feed, err := m.fetchFeed(url)
	if err != nil {
		return nil, err
	}
	var events []Event
	for _, entry := range feed.Entries {
		if isTicketNews(entry.Title, "") {
			events = append(events, Event{
				ID:        entry.ID,
				Title:     entry.Title,
				URL:       entry.Link.Href,
				Source:    "reddit",
				EventType: EventTypeAnnouncement,
			})
		}
	}
	return events, nil
}

func (m *RedditMonitor) fetchFeed(url string) (*atomFeed, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("reddit: %w", err)
	}
	req.Header.Set("User-Agent", "DotaTicketWatch/1.0")
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reddit fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reddit: status %d", resp.StatusCode)
	}
	var feed atomFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("reddit parse: %w", err)
	}
	return &feed, nil
}
