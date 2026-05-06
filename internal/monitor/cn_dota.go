package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/artem/dotaticketwatch/internal/ai"
)

type CNCategory string

const (
	CNCategoryPreSaleGlobal CNCategory = "pre_sale_global"
	CNCategoryPreSaleCNOnly CNCategory = "pre_sale_cn_only"
	CNCategoryMarketingCN   CNCategory = "marketing_cn"
	CNCategoryNotRelevant   CNCategory = "not_relevant"
)

type CNClassification struct {
	Category   CNCategory `json:"category"`
	Confidence float64    `json:"confidence"`
	TitleRu    string     `json:"title_ru"`
	NoteRu     string     `json:"note_ru"`
}

type CNHeadline struct {
	ArticleID string
	Date      string
	Title     string
	URL       string
}

type cnStore interface {
	AlreadyClassified(key string) bool
	MarkClassified(key string) error
}

type CNDotaMonitor struct {
	siteURL  string
	flareURL string
	fetchFn  func(url, flareSolverrURL string) (string, error)
	aiClient ai.Client
	store    cnStore
	adminFn  func(string)
}

func NewCNDotaMonitor(siteURL, flareURL string, fetchFn func(string, string) (string, error), aiClient ai.Client, store cnStore, adminFn func(string)) *CNDotaMonitor {
	return &CNDotaMonitor{
		siteURL:  siteURL,
		flareURL: flareURL,
		fetchFn:  fetchFn,
		aiClient: aiClient,
		store:    store,
		adminFn:  adminFn,
	}
}

func (m *CNDotaMonitor) Name() string { return "CN/dota2.com.cn" }

func (m *CNDotaMonitor) FetchHTML() (string, error) {
	return m.fetchFn(m.siteURL, m.flareURL)
}

func (m *CNDotaMonitor) Check() ([]Event, error) {
	if m.aiClient == nil || !m.aiClient.IsEnabled() {
		return nil, nil
	}
	html, err := m.fetchFn(m.siteURL, m.flareURL)
	if err != nil {
		return nil, fmt.Errorf("cn fetch: %w", err)
	}
	headlines := ExtractCNHeadlines(html)
	var events []Event
	for _, h := range headlines {
		key := "cn:" + h.ArticleID
		if m.store != nil && m.store.AlreadyClassified(key) {
			continue
		}
		cls, err := ClassifyCNContent(context.Background(), m.aiClient, h.Title)
		if err != nil {
			slog.Warn("cn classify failed", "article", h.ArticleID, "err", err)
			continue
		}
		if m.store != nil {
			_ = m.store.MarkClassified(key)
		}
		switch cls.Category {
		case CNCategoryPreSaleGlobal:
			if cls.Confidence < 0.7 {
				continue
			}
			events = append(events, Event{
				ID:        "cn-" + h.ArticleID,
				Title:     fmt.Sprintf("[cn] %s", cls.TitleRu),
				URL:       h.URL,
				Source:    "cn",
				EventType: EventTypeSale,
			})
		case CNCategoryPreSaleCNOnly:
			if cls.Confidence < 0.7 || m.adminFn == nil {
				continue
			}
			m.adminFn(fmt.Sprintf(
				"▸ <b>cn-only</b> · только китайский канал\n<i>%s</i>\n<i>%s</i>\n<a href=\"%s\">источник</a>",
				cls.TitleRu, cls.NoteRu, h.URL))
		case CNCategoryMarketingCN:
			if cls.Confidence < 0.8 || m.adminFn == nil {
				continue
			}
			m.adminFn(fmt.Sprintf("· <b>cn-promo</b>\n<i>%s</i>", cls.TitleRu))
		}
	}
	return events, nil
}

var cnHeadlineRegex = regexp.MustCompile(`article/details/(\d{8})/(\d+)\.html[\s\S]{0,400}?class="title"[^>]*>([^<]+)`)

func ExtractCNHeadlines(html string) []CNHeadline {
	matches := cnHeadlineRegex.FindAllStringSubmatch(html, -1)
	seen := map[string]bool{}
	var out []CNHeadline
	for _, m := range matches {
		date, id, title := m[1], m[2], m[3]
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, CNHeadline{
			ArticleID: id,
			Date:      date,
			Title:     title,
			URL:       fmt.Sprintf("https://www.dota2.com.cn/article/details/%s/%s.html", date, id),
		})
	}
	return out
}

func ClassifyCNContent(ctx context.Context, c ai.Client, title string) (*CNClassification, error) {
	if c == nil || !c.IsEnabled() {
		return nil, ai.ErrAINotEnabled
	}
	resp, err := c.Complete(ctx, ai.CompleteRequest{
		Model:     c.ModelFast(),
		System:    ai.SystemCNClassify,
		User:      title,
		JSONMode:  true,
		MaxTokens: 200,
		CacheKey:  ai.HashKey("cn_classify", c.ModelFast(), ai.SystemCNClassify, title),
	})
	if err != nil {
		return nil, err
	}
	var cls CNClassification
	if err := json.Unmarshal([]byte(resp.Content), &cls); err != nil {
		return nil, fmt.Errorf("cn classify parse: %w (raw: %s)", err, resp.Content)
	}
	return &cls, nil
}
