package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/artem/dotaticketwatch/internal/ai"
)

type steamStore interface {
	AlreadyClassified(key string) bool
	MarkClassified(key string) error
}

var ticketSignals = []string{
	"tickets", "ticket sale", "on sale", "presale", "pre-sale",
	"spectator pass", "viewer pass", "axs",
}

var eventSignals = []string{
	"the international", "ti 2026", "ti2026",
}

var steamImagePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\[img\](https?://[^\]\s]+)\[/img\]`),
	regexp.MustCompile(`\[img\]\{STEAM_CLAN_IMAGE\}/(\S+)\[/img\]`),
	regexp.MustCompile(`<img[^>]+src="(https?://[^"]+)"`),
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
	apiURL   string
	client   *http.Client
	aiClient ai.Client
	store    steamStore
}

func NewSteamNewsMonitor(apiURL string, aiClient ai.Client, store steamStore) *SteamNewsMonitor {
	return &SteamNewsMonitor{
		apiURL:   apiURL,
		client:   &http.Client{Timeout: 15 * time.Second},
		aiClient: aiClient,
		store:    store,
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
		signal, source := m.classify(item)
		if !signal {
			continue
		}
		events = append(events, Event{
			ID:        item.GID,
			Title:     item.Title,
			URL:       item.URL,
			Source:    source,
			EventType: EventTypeAnnouncement,
			ImageURL:  extractSteamImage(item.Contents),
		})
	}
	return events, nil
}

func (m *SteamNewsMonitor) classify(item steamNewsItem) (bool, string) {
	keywordHit := isTicketNews(item.Title, item.Contents)

	if m.aiClient == nil || !m.aiClient.IsEnabled() {
		if keywordHit {
			return true, "steam"
		}
		return false, ""
	}

	if !keywordHit && !mentionsTI(item.Title, item.Contents) {
		return false, ""
	}

	classifyKey := "steam:" + item.GID
	if m.store != nil && m.store.AlreadyClassified(classifyKey) {
		if keywordHit {
			return true, "steam"
		}
		return false, ""
	}

	cls, err := ClassifySteamPost(context.Background(), m.aiClient, item.Title, item.Contents)
	if err != nil {
		slog.Warn("steam ai classify failed", "gid", item.GID, "err", err)
		if keywordHit {
			return true, "steam"
		}
		return false, ""
	}
	if m.store != nil {
		_ = m.store.MarkClassified(classifyKey)
	}
	if cls.IsTicketSignal && cls.Confidence >= 0.6 {
		if keywordHit {
			return true, "steam"
		}
		return true, "steam-ai"
	}
	return false, ""
}

func mentionsTI(title, contents string) bool {
	text := strings.ToLower(title + " " + contents)
	signals := []string{"the international", "ti 2026", "ti2026", "ti26", "shanghai", "international 2026"}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
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

const steamClanImageBase = "https://clan.akamai.steamstatic.com/images/"

var trustedImageHosts = []string{
	"clan.akamai.steamstatic.com",
	"cdn.cloudflare.steamstatic.com",
	"clan.cloudflare.steamstatic.com",
	"steamcdn-a.akamaihd.net",
	"store.steampowered.com",
	"cdn.dota2.com",
	"www.dota2.com",
}

func extractSteamImage(contents string) string {
	for _, re := range steamImagePatterns {
		if m := re.FindStringSubmatch(contents); m != nil {
			url := m[1]
			if !strings.HasPrefix(url, "http") {
				url = steamClanImageBase + url
			}
			if isTrustedImageHost(url) {
				return url
			}
		}
	}
	return ""
}

func isTrustedImageHost(url string) bool {
	lower := strings.ToLower(url)
	for _, host := range trustedImageHosts {
		if strings.Contains(lower, host) {
			return true
		}
	}
	return false
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
