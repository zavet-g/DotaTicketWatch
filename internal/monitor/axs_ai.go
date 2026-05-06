package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/artem/dotaticketwatch/internal/ai"
)

const axsHTMLMaxLen = 60000

var (
	scriptBlockRegex = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleStripRegex  = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	dataURIRegex     = regexp.MustCompile(`data:image/[^"' ]+`)
	whitespaceRegex  = regexp.MustCompile(`\s{3,}`)
	axsIDRegex       = regexp.MustCompile(`^\d{5,8}$`)
)

type aiAXSEvent struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Date  string `json:"date"`
	URL   string `json:"url"`
}

type aiAXSResponse struct {
	Events []aiAXSEvent `json:"events"`
}

func ParseAXSWithAI(ctx context.Context, c ai.Client, html string) ([]Event, error) {
	if c == nil || !c.IsEnabled() {
		return nil, ai.ErrAINotEnabled
	}
	clean := sanitizeAXSHTML(html)
	resp, err := c.Complete(ctx, ai.CompleteRequest{
		Model:     c.ModelFast(),
		System:    ai.SystemAXSFallback,
		User:      clean,
		JSONMode:  true,
		MaxTokens: 600,
		CacheKey:  ai.HashKey("axs_fallback", c.ModelFast(), clean),
	})
	if err != nil {
		return nil, err
	}
	var parsed aiAXSResponse
	if err := json.Unmarshal([]byte(resp.Content), &parsed); err != nil {
		return nil, fmt.Errorf("axs ai parse: %w (raw: %s)", err, resp.Content)
	}
	var events []Event
	seen := map[string]bool{}
	for _, e := range parsed.Events {
		id := strings.TrimSpace(e.ID)
		if !axsIDRegex.MatchString(id) || seen[id] {
			continue
		}
		seen[id] = true
		title := e.Title
		if title == "" {
			title = "The International 2026 — билеты в продаже"
		}
		url := e.URL
		if url == "" {
			url = fmt.Sprintf("https://www.axs.com/events/%s/the-international-dota-2-tickets", id)
		}
		events = append(events, Event{
			ID:        id,
			Title:     title,
			URL:       url,
			Source:    "axs-ai",
			EventType: EventTypeSale,
		})
	}
	return events, nil
}

func sanitizeAXSHTML(html string) string {
	out := scriptBlockRegex.ReplaceAllStringFunc(html, func(block string) string {
		if strings.Contains(block, "__NEXT_DATA__") {
			return block
		}
		return ""
	})
	out = styleStripRegex.ReplaceAllString(out, "")
	out = dataURIRegex.ReplaceAllString(out, "")
	out = whitespaceRegex.ReplaceAllString(out, " ")
	if len(out) > axsHTMLMaxLen {
		out = out[:axsHTMLMaxLen]
	}
	return out
}
