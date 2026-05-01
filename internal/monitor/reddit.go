package monitor

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
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
		if isRedditTicketAnnouncement(entry.Title) {
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

var redditEventSignals = []string{
	"the international",
	"international 2026",
	"ti 2026", "ti2026",
	"ti 26", "ti26",
}

var redditTicketSignals = []string{
	"tickets", "ticket", "ticketing",
	"presale", "pre-sale",
	"spectator pass", "viewer pass",
	"general sale", "general admission",
	"axs",
}

var redditSaleSignals = []string{
	"on sale", "for sale", "now selling", "are selling",
	"now available", "available now",
	"are live", "is live", "go live", "goes live", "went live", "live now",
	"just dropped", "tickets dropped", "dropped today",
	"are out", "out now",
	"release date", "sale date", "sale time", "date confirmed",
	"presale begins", "presale opens", "presale starts", "presale is live",
	"pre-sale begins", "pre-sale opens", "pre-sale starts", "pre-sale is live",
	"sale begins", "sale opens", "sale starts", "sale is live",
	"sales begin", "sales open", "sales start", "sales are live",
	"ticket sale", "ticket sales",
	"tickets for the international",
	"buy now", "buy your tickets", "get your tickets",
	"axs.com", "powered by axs", "partnership with axs",
	"tickets announced", "tickets revealed",
	"info revealed", "info announced",
	"prices revealed", "prices announced",
	"details revealed", "details announced",
	"ticketing partner", "ticketing faq",
	"first wave", "wave 1", "in waves",
	"general sale", "general admission",
	"save the date", "sold out",
}

var redditNoiseSignals = []string{
	"discussion", "discuss", "megathread",
	"rumor", "rumour", "speculation", "speculate",
	"wishlist", "hopium", "copium",
	"what if", "anyone else", "hopefully", "i wish", "if only",
	"expected to", "rumored to", "gonna be",
	"meme", "fluff", "shitpost", "joke", "lol",
	"circlejerk", "unpopular opinion", "hot take",
	"nightmare", "armageddon", "woes", "furious", "fuming",
	"[question]", "[help]", "[fluff]", "[meme]", "[clip]", "[fan art]",
	"fake news", "hoax", " /s",
}

func isRedditTicketAnnouncement(title string) bool {
	if strings.Contains(title, "?") {
		return false
	}
	lower := strings.ToLower(title)
	for _, s := range redditNoiseSignals {
		if strings.Contains(lower, s) {
			return false
		}
	}
	if !containsAny(lower, redditEventSignals) {
		return false
	}
	if !containsAny(lower, redditTicketSignals) {
		return false
	}
	return containsAny(lower, redditSaleSignals)
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
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
