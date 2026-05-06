package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/artem/dotaticketwatch/internal/ai"
	"github.com/artem/dotaticketwatch/internal/config"
	"github.com/artem/dotaticketwatch/internal/fetcher"
	"github.com/artem/dotaticketwatch/internal/monitor"
	"github.com/artem/dotaticketwatch/internal/storage"
)

func handleAITest(msg *tgbotapi.Message, bot *tgbotapi.BotAPI, cfg *config.Config, store *storage.Storage, aiClient ai.Client) {
	chatID := msg.Chat.ID
	if cfg.AdminChatID == 0 || chatID != cfg.AdminChatID {
		return
	}
	args := strings.Fields(msg.CommandArguments())
	if !aiClient.IsEnabled() {
		reply(bot, chatID, "× ии не настроен · установи <code>OPENAI_API_KEY</code>")
		return
	}
	if len(args) == 0 {
		reply(bot, chatID, aitestMenu())
		return
	}
	switch args[0] {
	case "health":
		go aitestHealth(bot, chatID, aiClient)
	case "axs":
		broken := len(args) > 1 && args[1] == "broken"
		go aitestAXS(bot, chatID, cfg, aiClient, broken)
	case "steam":
		go aitestSteam(bot, chatID, cfg, aiClient)
	case "diff":
		mock := len(args) > 1 && args[1] == "mock"
		go aitestDiff(bot, chatID, cfg, store, aiClient, mock)
	case "cn":
		mode := ""
		if len(args) > 1 {
			mode = args[1]
		}
		go aitestCN(bot, chatID, cfg, aiClient, mode)
	default:
		reply(bot, chatID, "× неизвестная команда\n\n"+aitestMenu())
	}
}

func aitestMenu() string {
	return strings.Join([]string{
		"▸ <b>ai тесты</b>",
		"доступно:",
		"· /aitest health     — пинг ии-клиента",
		"· /aitest axs        — fallback парсер на реальном html",
		"· /aitest axs broken — на сломанном html",
		"· /aitest steam      — классификатор news",
		"· /aitest diff       — axs __NEXT_DATA__ diff",
		"· /aitest diff mock",
		"· /aitest cn         — китайский watcher",
		"· /aitest cn mock global",
		"· /aitest cn mock cnonly",
		"· /aitest cn mock marketing",
	}, "\n")
}

func aitestHealth(bot *tgbotapi.BotAPI, chatID int64, aiClient ai.Client) {
	start := time.Now()
	resp, err := aiClient.Complete(context.Background(), ai.CompleteRequest{
		Model:     aiClient.ModelFast(),
		User:      "pong?",
		MaxTokens: 10,
	})
	if err != nil {
		reply(bot, chatID, fmt.Sprintf("× <b>health</b>\n<code>%v</code>", err))
		return
	}
	dt := time.Since(start).Round(time.Millisecond)
	reply(bot, chatID, fmt.Sprintf(
		"· <b>health</b> ok\n<i>%s</i>\n<code>tokens · %d in / %d out · $%.5f</code>\n<code>%s</code>",
		strings.TrimSpace(resp.Content),
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.CostUSD,
		dt))
}

func aitestAXS(bot *tgbotapi.BotAPI, chatID int64, cfg *config.Config, aiClient ai.Client, broken bool) {
	var html string
	var err error
	if broken {
		html = `<!DOCTYPE html><html><body>
<h1>The International 2026 — Tickets</h1>
<p>Get ready for TI 2026 in Shanghai!</p>
<a href="https://www.axs.com/events/9999991/the-international-dota-2-tickets">Buy tickets</a>
<a href="https://www.axs.com/events/9999992/ti-finals">Finals tickets</a>
</body></html>`
	} else {
		html, err = fetcher.Fetch(cfg.AXSHubURL, cfg.FlareSolverrURL)
		if err != nil {
			reply(bot, chatID, fmt.Sprintf("× fetch axs\n<code>%v</code>", err))
			return
		}
	}

	keywordEvents, _ := extractAXSEventsHelper(html)
	start := time.Now()
	aiEvents, aiErr := monitor.ParseAXSWithAI(context.Background(), aiClient, html)
	dt := time.Since(start).Round(time.Millisecond)

	var sb strings.Builder
	if broken {
		sb.WriteString("▸ <b>AXS · ai на сломанном html</b>\n")
	} else {
		sb.WriteString("▸ <b>AXS · ai-fallback тест</b>\n")
	}
	sb.WriteString(fmt.Sprintf("· keyword: %d событий\n", len(keywordEvents)))
	if aiErr != nil {
		sb.WriteString(fmt.Sprintf("× ai: %v\n", aiErr))
	} else {
		sb.WriteString(fmt.Sprintf("▸ ai: %d событий\n", len(aiEvents)))
		for _, e := range aiEvents {
			sb.WriteString(fmt.Sprintf("  · <code>%s</code> %s\n", e.ID, truncateLine(e.Title, 60)))
		}
		matchKeyword := sameIDs(keywordEvents, aiEvents)
		if matchKeyword && !broken {
			sb.WriteString("· совпадает\n")
		}
	}
	sb.WriteString(fmt.Sprintf("<code>%s</code>", dt))
	reply(bot, chatID, sb.String())
}

func aitestSteam(bot *tgbotapi.BotAPI, chatID int64, cfg *config.Config, aiClient ai.Client) {
	m := monitor.NewSteamNewsMonitor(cfg.SteamNewsURL, nil, nil)
	items, err := m.FetchItems()
	if err != nil {
		reply(bot, chatID, fmt.Sprintf("× steam fetch\n<code>%v</code>", err))
		return
	}
	if len(items) > 20 {
		items = items[:20]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("▸ <b>Steam · ai-классификатор</b>\nпроанализировано %d постов\n\n", len(items)))

	var totalCost float64
	var diffs int
	for _, item := range items {
		kw := monitor.IsTicketNewsExported(item.Title, item.Contents)
		var aiHit bool
		var aiNote string
		if kw || mentionsTIExt(item.Title, item.Contents) {
			cls, err := monitor.ClassifySteamPost(context.Background(), aiClient, item.Title, item.Contents)
			if err != nil {
				aiNote = fmt.Sprintf("× %v", err)
			} else {
				aiHit = cls.IsTicketSignal && cls.Confidence >= 0.6
				aiNote = fmt.Sprintf("%.2f", cls.Confidence)
			}
		}
		marker := "·"
		if kw != aiHit {
			marker = "▸"
			diffs++
		}
		title := truncateLine(item.Title, 50)
		sb.WriteString(fmt.Sprintf("%s %q  kw:%s ai:%s  %s\n",
			marker, title, yn(kw), yn(aiHit), aiNote))
	}
	sb.WriteString(fmt.Sprintf("\n· keyword/ai diff: %d постов\n", diffs))
	sb.WriteString(fmt.Sprintf("<code>cost ≈ $%.4f</code>", totalCost))

	out := sb.String()
	if len(out) > 4000 {
		out = out[:4000] + "\n…"
	}
	reply(bot, chatID, out)
}

func aitestDiff(bot *tgbotapi.BotAPI, chatID int64, cfg *config.Config, store *storage.Storage, aiClient ai.Client, mock bool) {
	if mock {
		oldJSON := `{"performerEvents":{"totalEvents":0,"eventItems":[]},"teamUpcoming":{"hasUpcomingEvent":false},"discovery":{"events":[]}}`
		newJSON := `{"performerEvents":{"totalEvents":1,"eventItems":[{"id":"7654321","eventName":"The International 2026","statusId":2,"venueCity":"Shanghai","date":"2026-08-20"}]},"teamUpcoming":{"hasUpcomingEvent":true,"upcomingEvent":{"id":"7654321","eventName":"TI 2026"}},"discovery":{"events":[{"id":"7654321"}]}}`
		start := time.Now()
		sig, err := monitor.AnalyzeAXSDiff(context.Background(), aiClient, oldJSON, newJSON)
		dt := time.Since(start).Round(time.Millisecond)
		if err != nil {
			reply(bot, chatID, fmt.Sprintf("× diff mock\n<code>%v</code>", err))
			return
		}
		var sb strings.Builder
		sb.WriteString("▸ <b>AXS diff · mock</b>\n")
		sb.WriteString(fmt.Sprintf("· is_pre_sale_signal: %v\n", sig.IsPreSaleSignal))
		sb.WriteString(fmt.Sprintf("· confidence: %.2f\n", sig.Confidence))
		sb.WriteString(fmt.Sprintf("<i>%s</i>\n", sig.Summary))
		for _, ch := range sig.Changes {
			sb.WriteString(fmt.Sprintf("• %s\n", ch))
		}
		sb.WriteString(fmt.Sprintf("<code>%s</code>", dt))
		reply(bot, chatID, sb.String())
		return
	}

	html, err := fetcher.Fetch(cfg.AXSHubURL, cfg.FlareSolverrURL)
	if err != nil {
		reply(bot, chatID, fmt.Sprintf("× axs fetch\n<code>%v</code>", err))
		return
	}
	pruned, err := monitor.PruneAXSSnapshotExported(html)
	if err != nil {
		reply(bot, chatID, fmt.Sprintf("× prune snapshot\n<code>%v</code>", err))
		return
	}
	prev, ok := store.AIStateGet("axs_snapshot")
	_ = store.AIStateSet("axs_snapshot", pruned)
	if !ok {
		reply(bot, chatID, "· базовый снапшот сохранён · повтори через 5 минут")
		return
	}
	if string(prev) == string(pruned) {
		reply(bot, chatID, "▸ <b>AXS diff</b>\n· идентично (no ai call)\n· ok")
		return
	}
	start := time.Now()
	sig, err := monitor.AnalyzeAXSDiff(context.Background(), aiClient, string(prev), string(pruned))
	dt := time.Since(start).Round(time.Millisecond)
	if err != nil {
		reply(bot, chatID, fmt.Sprintf("× diff ai\n<code>%v</code>", err))
		return
	}
	var sb strings.Builder
	sb.WriteString("▸ <b>AXS diff</b>\n")
	sb.WriteString(fmt.Sprintf("· is_pre_sale_signal: %v\n", sig.IsPreSaleSignal))
	sb.WriteString(fmt.Sprintf("· confidence: %.2f\n", sig.Confidence))
	if sig.Summary != "" {
		sb.WriteString(fmt.Sprintf("<i>%s</i>\n", sig.Summary))
	}
	for _, ch := range sig.Changes {
		sb.WriteString(fmt.Sprintf("• %s\n", ch))
	}
	sb.WriteString(fmt.Sprintf("<code>%s</code>", dt))
	reply(bot, chatID, sb.String())
}

func aitestCN(bot *tgbotapi.BotAPI, chatID int64, cfg *config.Config, aiClient ai.Client, mode string) {
	mockTitles := map[string]string{
		"global":    "国际邀请赛2026门票将于八月开售（axs）",
		"cnonly":    "大麦网 ti2026 门票预售开启",
		"marketing": "抽取ti2026观赛门票",
	}
	if mode == "mock" {
		reply(bot, chatID, "× укажи: cn mock global | cnonly | marketing")
		return
	}
	if title, ok := mockTitles[mode]; ok {
		start := time.Now()
		cls, err := monitor.ClassifyCNContent(context.Background(), aiClient, title)
		dt := time.Since(start).Round(time.Millisecond)
		if err != nil {
			reply(bot, chatID, fmt.Sprintf("× cn mock %s\n<code>%v</code>", mode, err))
			return
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("▸ <b>CN · mock %s</b>\n", mode))
		sb.WriteString(fmt.Sprintf("input: <code>%s</code>\n\n", title))
		sb.WriteString(fmt.Sprintf("· category: <b>%s</b>\n", cls.Category))
		sb.WriteString(fmt.Sprintf("· confidence: %.2f\n", cls.Confidence))
		sb.WriteString(fmt.Sprintf("· title_ru: %s\n", cls.TitleRu))
		if cls.NoteRu != "" {
			sb.WriteString(fmt.Sprintf("· note_ru: <i>%s</i>\n", cls.NoteRu))
		}
		sb.WriteString(fmt.Sprintf("<code>%s</code>", dt))
		reply(bot, chatID, sb.String())
		return
	}

	html, err := fetcher.Fetch(cfg.CNNewsURL, cfg.FlareSolverrURL)
	if err != nil {
		reply(bot, chatID, fmt.Sprintf("× cn fetch\n<code>%v</code>", err))
		return
	}
	headlines := monitor.ExtractCNHeadlines(html)
	if len(headlines) > 12 {
		headlines = headlines[:12]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("▸ <b>CN · dota2.com.cn</b>\nнайдено %d заголовков\n\n", len(headlines)))

	var counts = map[CNCategoryStat]int{}
	var totalCost float64
	for _, h := range headlines {
		cls, err := monitor.ClassifyCNContent(context.Background(), aiClient, h.Title)
		if err != nil {
			sb.WriteString(fmt.Sprintf("× <code>%s</code>: %v\n", h.ArticleID, err))
			continue
		}
		counts[CNCategoryStat(cls.Category)]++
		sb.WriteString(fmt.Sprintf("· <b>%s</b> (%.2f)\n  %s\n", cls.Category, cls.Confidence, cls.TitleRu))
		if cls.NoteRu != "" {
			sb.WriteString(fmt.Sprintf("  <i>%s</i>\n", cls.NoteRu))
		}
	}
	sb.WriteString(fmt.Sprintf("\n· итог: pre_sale_global=%d cn_only=%d marketing=%d not_relevant=%d\n",
		counts["pre_sale_global"], counts["pre_sale_cn_only"], counts["marketing_cn"], counts["not_relevant"]))
	sb.WriteString(fmt.Sprintf("<code>cost ≈ $%.4f</code>", totalCost))

	out := sb.String()
	if len(out) > 4000 {
		out = out[:4000] + "\n…"
	}
	reply(bot, chatID, out)
}

type CNCategoryStat string

func extractAXSEventsHelper(html string) ([]monitor.Event, error) {
	return monitor.ExtractAXSEventsExported(html)
}

func sameIDs(a, b []monitor.Event) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, e := range a {
		seen[e.ID] = true
	}
	for _, e := range b {
		if !seen[e.ID] {
			return false
		}
	}
	return true
}

func truncateLine(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func yn(b bool) string {
	if b {
		return "y"
	}
	return "n"
}

func mentionsTIExt(title, contents string) bool {
	text := strings.ToLower(title + " " + contents)
	for _, s := range []string{"the international", "ti 2026", "ti2026", "shanghai", "international 2026"} {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}
