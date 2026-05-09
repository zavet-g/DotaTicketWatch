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

const aitestMaxLen = 3900

func handleAITest(msg *tgbotapi.Message, bot *tgbotapi.BotAPI, cfg *config.Config, store *storage.Storage, aiClient ai.Client) {
	chatID := msg.Chat.ID
	if cfg.AdminChatID == 0 || chatID != cfg.AdminChatID {
		return
	}
	if !aiClient.IsEnabled() {
		reply(bot, chatID, "× ии не настроен · установи <code>OPENAI_API_KEY</code>")
		return
	}
	go aitestAll(bot, chatID, cfg, store, aiClient)
}

func aitestAll(bot *tgbotapi.BotAPI, chatID int64, cfg *config.Config, store *storage.Storage, aiClient ai.Client) {
	header := "▸ <b>ai тесты</b> · полный прогон\n<i>9 шагов · обновление по мере готовности</i>\n\n"
	initMsg, err := bot.Send(newHTMLMessage(chatID, header))
	if err != nil {
		return
	}
	msgID := initMsg.MessageID

	steps := []struct {
		name string
		run  func() string
	}{
		{"health", func() string { return runHealth(aiClient) }},
		{"axs broken", func() string { return runAXS(cfg, aiClient, true) }},
		{"axs real", func() string { return runAXS(cfg, aiClient, false) }},
		{"steam", func() string { return runSteam(cfg, aiClient) }},
		{"cn real", func() string { return runCN(cfg, aiClient) }},
		{"cn mock global", func() string { return runCNMock(aiClient, "global") }},
		{"cn mock cnonly", func() string { return runCNMock(aiClient, "cnonly") }},
		{"cn mock marketing", func() string { return runCNMock(aiClient, "marketing") }},
		{"diff mock", func() string { return runDiffMock(aiClient) }},
	}

	var sb strings.Builder
	sb.WriteString(header)
	start := time.Now()

	for _, st := range steps {
		block := st.run()
		sb.WriteString(block)
		sb.WriteString("\n\n")
		editAITest(bot, chatID, msgID, sb.String())
	}

	sb.WriteString(fmt.Sprintf("· <b>готово</b> · %s", time.Since(start).Round(time.Second)))
	editAITest(bot, chatID, msgID, sb.String())
}

func editAITest(bot *tgbotapi.BotAPI, chatID int64, msgID int, text string) {
	if len(text) > aitestMaxLen {
		text = text[:aitestMaxLen] + "\n…"
	}
	editMessage(bot, chatID, msgID, text)
}

func runHealth(aiClient ai.Client) string {
	start := time.Now()
	resp, err := aiClient.Complete(context.Background(), ai.CompleteRequest{
		Model:     aiClient.ModelFast(),
		User:      "pong?",
		MaxTokens: 10,
	})
	dt := time.Since(start).Round(time.Millisecond)
	if err != nil {
		return fmt.Sprintf("× <b>health</b>\n<code>%v</code>", err)
	}
	cached := ""
	if resp.Usage.Cached {
		cached = " · cached"
	}
	return fmt.Sprintf(
		"· <b>health</b> · <i>%s</i>\n<code>%dt · $%.5f%s · %s</code>",
		strings.TrimSpace(resp.Content),
		resp.Usage.TotalTokens, resp.Usage.CostUSD, cached, dt)
}

func runAXS(cfg *config.Config, aiClient ai.Client, broken bool) string {
	var html string
	var err error
	label := "AXS · ai-fallback"
	if broken {
		label = "AXS · сломанный html"
		html = `<!DOCTYPE html><html><body>
<h1>The International 2026 — Tickets</h1>
<a href="https://www.axs.com/events/9999991/the-international-dota-2-tickets">Buy</a>
<a href="https://www.axs.com/events/9999992/ti-finals">Finals</a>
</body></html>`
	} else {
		html, err = fetcher.Fetch(cfg.AXSHubURL, cfg.FlareSolverrURL)
		if err != nil {
			return fmt.Sprintf("× <b>%s</b>\n<code>%v</code>", label, err)
		}
	}

	keywordEvents, _ := monitor.ExtractAXSEventsExported(html)
	start := time.Now()
	aiEvents, aiErr := monitor.ParseAXSWithAI(context.Background(), aiClient, html, monitor.HubTeamIDFromURL(cfg.AXSHubURL))
	dt := time.Since(start).Round(time.Millisecond)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("▸ <b>%s</b>\n", label))
	sb.WriteString(fmt.Sprintf("· keyword: %d · ai: ", len(keywordEvents)))
	if aiErr != nil {
		sb.WriteString(fmt.Sprintf("× %v", aiErr))
	} else {
		sb.WriteString(fmt.Sprintf("%d", len(aiEvents)))
		for _, e := range aiEvents {
			sb.WriteString(fmt.Sprintf("\n  <code>%s</code> %s", e.ID, truncateLine(e.Title, 50)))
		}
		if !broken && sameIDs(keywordEvents, aiEvents) {
			sb.WriteString(" · совпадает")
		}
	}
	sb.WriteString(fmt.Sprintf("\n<code>%s</code>", dt))
	return sb.String()
}

func runSteam(cfg *config.Config, aiClient ai.Client) string {
	m := monitor.NewSteamNewsMonitor(cfg.SteamNewsURL, nil, nil)
	items, err := m.FetchItems()
	if err != nil {
		return fmt.Sprintf("× <b>Steam</b>\n<code>%v</code>", err)
	}
	if len(items) > 20 {
		items = items[:20]
	}
	start := time.Now()

	var diffs int
	var classified int
	type row struct {
		title  string
		kw     bool
		ai     bool
		conf   float64
	}
	var diffRows []row
	for _, item := range items {
		kw := monitor.IsTicketNewsExported(item.Title, item.Contents)
		var aiHit bool
		var conf float64
		if kw || mentionsTIExt(item.Title, item.Contents) {
			classified++
			cls, err := monitor.ClassifySteamPost(context.Background(), aiClient, item.Title, item.Contents)
			if err == nil {
				aiHit = cls.IsTicketSignal && cls.Confidence >= 0.6
				conf = cls.Confidence
			}
		}
		if kw != aiHit {
			diffs++
			diffRows = append(diffRows, row{
				title: truncateLine(item.Title, 50),
				kw:    kw, ai: aiHit, conf: conf,
			})
		}
	}
	dt := time.Since(start).Round(time.Millisecond)

	var sb strings.Builder
	sb.WriteString("▸ <b>Steam · ai-классификатор</b>\n")
	sb.WriteString(fmt.Sprintf("постов %d · ии-вызовов %d · diff %d\n", len(items), classified, diffs))
	for _, r := range diffRows {
		sb.WriteString(fmt.Sprintf("· %s · kw:%s ai:%s (%.2f)\n", r.title, yn(r.kw), yn(r.ai), r.conf))
	}
	sb.WriteString(fmt.Sprintf("<code>%s</code>", dt))
	return sb.String()
}

func runCN(cfg *config.Config, aiClient ai.Client) string {
	html, err := fetcher.Fetch(cfg.CNNewsURL, cfg.FlareSolverrURL)
	if err != nil {
		return fmt.Sprintf("× <b>CN · fetch</b>\n<code>%v</code>", err)
	}
	headlines := monitor.ExtractCNHeadlines(html)
	if len(headlines) > 10 {
		headlines = headlines[:10]
	}
	start := time.Now()

	counts := map[string]int{}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("▸ <b>CN · dota2.com.cn</b>\n%d заголовков\n", len(headlines)))
	for _, h := range headlines {
		cls, err := monitor.ClassifyCNContent(context.Background(), aiClient, h.Title)
		if err != nil {
			sb.WriteString(fmt.Sprintf("× %s\n", h.ArticleID))
			continue
		}
		counts[string(cls.Category)]++
		sb.WriteString(fmt.Sprintf("· %s (%.2f) %s\n", cnCatShort(cls.Category), cls.Confidence, truncateLine(cls.TitleRu, 60)))
	}
	dt := time.Since(start).Round(time.Millisecond)
	sb.WriteString(fmt.Sprintf("итог: gl=%d cn=%d mk=%d ¬=%d\n",
		counts["pre_sale_global"], counts["pre_sale_cn_only"], counts["marketing_cn"], counts["not_relevant"]))
	sb.WriteString(fmt.Sprintf("<code>%s</code>", dt))
	return sb.String()
}

func runCNMock(aiClient ai.Client, mode string) string {
	mockTitles := map[string]string{
		"global":    "国际邀请赛2026门票将于八月开售（axs）",
		"cnonly":    "大麦网 ti2026 门票预售开启",
		"marketing": "报名参赛刀塔校友会，抽取TI2026现场观赛门票！",
	}
	title, ok := mockTitles[mode]
	if !ok {
		return fmt.Sprintf("× cn mock %s · нет такого", mode)
	}
	start := time.Now()
	cls, err := monitor.ClassifyCNContent(context.Background(), aiClient, title)
	dt := time.Since(start).Round(time.Millisecond)
	if err != nil {
		return fmt.Sprintf("× <b>CN mock %s</b>\n<code>%v</code>", mode, err)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("▸ <b>CN mock · %s</b>\n", mode))
	sb.WriteString(fmt.Sprintf("· <b>%s</b> (%.2f)\n", cls.Category, cls.Confidence))
	sb.WriteString(fmt.Sprintf("· %s\n", cls.TitleRu))
	if cls.NoteRu != "" {
		sb.WriteString(fmt.Sprintf("<i>%s</i>\n", cls.NoteRu))
	}
	sb.WriteString(fmt.Sprintf("<code>%s</code>", dt))
	return sb.String()
}

func runDiffMock(aiClient ai.Client) string {
	oldJSON := `{"performerEvents":{"totalEvents":0,"eventItems":[]},"teamUpcoming":{"hasUpcomingEvent":false},"discovery":{"events":[]}}`
	newJSON := `{"performerEvents":{"totalEvents":1,"eventItems":[{"id":"7654321","eventName":"The International 2026","statusId":2,"venueCity":"Shanghai","date":"2026-08-20"}]},"teamUpcoming":{"hasUpcomingEvent":true,"upcomingEvent":{"id":"7654321","eventName":"TI 2026"}},"discovery":{"events":[{"id":"7654321"}]}}`
	start := time.Now()
	sig, err := monitor.AnalyzeAXSDiff(context.Background(), aiClient, oldJSON, newJSON)
	dt := time.Since(start).Round(time.Millisecond)
	if err != nil {
		return fmt.Sprintf("× <b>AXS diff mock</b>\n<code>%v</code>", err)
	}
	var sb strings.Builder
	sb.WriteString("▸ <b>AXS diff mock</b>\n")
	sb.WriteString(fmt.Sprintf("· pre_sale: %v · %.2f\n", sig.IsPreSaleSignal, sig.Confidence))
	if sig.Summary != "" {
		sb.WriteString(fmt.Sprintf("<i>%s</i>\n", sig.Summary))
	}
	for _, ch := range sig.Changes {
		sb.WriteString(fmt.Sprintf("• %s\n", ch))
	}
	sb.WriteString(fmt.Sprintf("<code>%s</code>", dt))
	return sb.String()
}

func cnCatShort(c monitor.CNCategory) string {
	switch c {
	case monitor.CNCategoryPreSaleGlobal:
		return "global"
	case monitor.CNCategoryPreSaleCNOnly:
		return "cn-only"
	case monitor.CNCategoryMarketingCN:
		return "marketing"
	case monitor.CNCategoryNotRelevant:
		return "¬"
	}
	return string(c)
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
