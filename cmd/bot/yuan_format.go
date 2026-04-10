package main

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/artem/dotaticketwatch/internal/yuan"
)

func formatYuanParts(snap *yuan.Snapshot, msc *time.Location) []string {
	if snap == nil {
		return []string{"× курс недоступен · попробуй позже"}
	}

	rate := snap.Rate

	price := rate.Last
	if !rate.Trading || price == 0 {
		price = rate.PrevPrice
	}
	if price == 0 {
		return []string{"× курс недоступен · попробуй позже"}
	}

	header := "<b>почём юань</b>\n<i>в шанхае рубли не примут</i>"
	priceLine := fmt.Sprintf("· <b>1 ¥ = %.2f ₽</b>", price)

	parts := []string{header, priceLine}

	candles := snap.Candles
	if rate.Trading && len(candles) > 0 {
		today := time.Now().In(msc).Format("2006-01-02")
		if candles[len(candles)-1].Date == today {
			candles = candles[:len(candles)-1]
		}
	}
	if len(candles) > 5 {
		candles = candles[len(candles)-5:]
	}

	if len(candles) > 0 {
		weekAgo := candles[0].Close
		change := (price - weekAgo) / weekAgo * 100
		absChange := math.Abs(change)

		var trend string
		switch {
		case change <= -1:
			trend = fmt.Sprintf("<b>упал на %.1f%%</b> за неделю · <code>было %.2f</code>\n<i>шанхай дешевеет</i>", absChange, weekAgo)
		case change >= 1:
			trend = fmt.Sprintf("<b>вырос на %.1f%%</b> за неделю · <code>было %.2f</code>\n<i>шанхай дорожает — не тяни</i>", absChange, weekAgo)
		default:
			trend = fmt.Sprintf("без изменений за неделю · <code>%.2f</code>\n<i>курс стоит</i>", weekAgo)
		}
		parts = append(parts, trend)
	}

	parts = append(parts, "<i>биржевой курс · в банке +2–5%</i>")

	parts = append(parts, "<blockquote><i>в зале никто не считает юани.</i>\nв зале — <b>поднимают аегис.</b></blockquote>")

	return parts
}

func sendYuan(bot *tgbotapi.BotAPI, chatID int64, cache *yuan.Cache) {
	snap := cache.Get()
	parts := formatYuanParts(snap, moscow)
	if len(parts) == 0 {
		return
	}

	anim := tgbotapi.NewAnimation(chatID, tgbotapi.FileBytes{Name: "yuan.mp4", Bytes: yuanChartMP4})
	anim.Caption = parts[0]
	anim.ParseMode = tgbotapi.ModeHTML
	sent, err := bot.Send(anim)
	if err != nil {
		slog.Warn("sendYuan animation failed", "chat_id", chatID, "err", err)
		return
	}

	caption := parts[0]
	for i := 1; i < len(parts); i++ {
		time.Sleep(600 * time.Millisecond)
		caption += "\n\n" + parts[i]
		edit := tgbotapi.NewEditMessageCaption(chatID, sent.MessageID, caption)
		edit.ParseMode = tgbotapi.ModeHTML
		bot.Send(edit)
	}
}
