package main

import (
	"fmt"
	"log/slog"
	"math"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/artem/dotaticketwatch/internal/weather"
)

func formatWeatherParts(snap *weather.Snapshot) []string {
	if snap == nil || len(snap.Days) == 0 {
		return []string{"× погода недоступна · попробуй позже"}
	}

	header := "<b>небо над ареной</b>\n<i>шанхай не шепчет — давит.</i>"

	today := snap.Days[0]
	todayDate, _ := time.Parse("2006-01-02", today.Date)
	tempMin := int(math.Round(today.TempMin))
	tempMax := int(math.Round(today.TempMax))

	var todayBlock string
	todayBlock += fmt.Sprintf("<b>сегодня · %d %s · %d–%d°</b>\n", todayDate.Day(), monthsRu[todayDate.Month()], tempMin, tempMax)

	for _, p := range snap.Today {
		line := fmt.Sprintf("  %-7s <code>%d°</code>", p.Name, p.Temp)
		wmo := weather.LookupWMO(p.WeatherCode)
		switch {
		case p.PrecipProb > 60:
			line += fmt.Sprintf("  <b>▸ %s %d%%</b>", wmo.Text, p.PrecipProb)
		case p.PrecipProb >= 30:
			line += fmt.Sprintf("  %s %d%%", wmo.Text, p.PrecipProb)
		}
		todayBlock += line + "\n"
	}

	var restDays string
	for _, d := range snap.Days[1:] {
		t, err := time.Parse("2006-01-02", d.Date)
		if err != nil {
			continue
		}
		day := fmt.Sprintf("%2d %s", t.Day(), monthsRu[t.Month()])
		tMin := int(math.Round(d.TempMin))
		tMax := int(math.Round(d.TempMax))
		wmo := weather.LookupWMO(d.WeatherCode)

		line := fmt.Sprintf("  %s  <code>%d–%d°</code>", day, tMin, tMax)

		switch {
		case d.PrecipProb > 60:
			line += fmt.Sprintf("  <b>▸ %s %d%%</b>", wmo.Text, d.PrecipProb)
		case d.PrecipProb >= 30:
			line += fmt.Sprintf("  %s %d%%", wmo.Text, d.PrecipProb)
		}

		restDays += line + "\n"
	}

	week := "<blockquote expandable><b>ещё 9 дней</b>\n" + restDays + "</blockquote>"

	parts := []string{header, todayBlock, week}
	parts = append(parts, "<blockquote><i>к небу присматриваются заранее.</i></blockquote>")

	return parts
}

func sendWeather(bot *tgbotapi.BotAPI, chatID int64, cache *weather.Cache) {
	snap := cache.Get()
	parts := formatWeatherParts(snap)
	if len(parts) == 0 {
		return
	}

	anim := tgbotapi.NewAnimation(chatID, tgbotapi.FileBytes{Name: "weather.mp4", Bytes: weatherSkyMP4})
	anim.Caption = parts[0]
	anim.ParseMode = tgbotapi.ModeHTML
	sent, err := bot.Send(anim)
	if err != nil {
		slog.Warn("sendWeather animation failed", "chat_id", chatID, "err", err)
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
