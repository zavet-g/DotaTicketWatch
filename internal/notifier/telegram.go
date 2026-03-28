package notifier

import (
	"fmt"
	"html"
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/artem/dotaticketwatch/internal/monitor"
	"github.com/artem/dotaticketwatch/internal/storage"
)

type TelegramNotifier struct {
	bot     *tgbotapi.BotAPI
	storage *storage.Storage
}

func NewTelegramNotifier(bot *tgbotapi.BotAPI, store *storage.Storage) *TelegramNotifier {
	return &TelegramNotifier{bot: bot, storage: store}
}

func (n *TelegramNotifier) Notify(event monitor.Event) error {
	text := formatEvent(event)
	subs, err := n.storage.AllSubscribers()
	if err != nil {
		return fmt.Errorf("load subscribers: %w", err)
	}
	for _, sub := range subs {
		if event.ImageURL != "" {
			if n.trySendPhoto(sub.ChatID, event.ImageURL, text) {
				continue
			}
		}
		n.sendText(sub.ChatID, text)
	}
	return nil
}

func (n *TelegramNotifier) NotifyText(text string) error {
	subs, err := n.storage.AllSubscribers()
	if err != nil {
		return fmt.Errorf("load subscribers: %w", err)
	}
	for _, sub := range subs {
		n.sendText(sub.ChatID, text)
	}
	return nil
}

func (n *TelegramNotifier) trySendPhoto(chatID int64, imageURL, caption string) bool {
	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(imageURL))
	photo.Caption = caption
	photo.ParseMode = tgbotapi.ModeHTML
	if _, err := n.bot.Send(photo); err != nil {
		slog.Warn("sendPhoto failed, falling back to text", "chat_id", chatID, "err", err)
		return false
	}
	return true
}

func (n *TelegramNotifier) sendText(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.DisableWebPagePreview = true
	if _, err := n.bot.Send(msg); err != nil {
		slog.Warn("failed to send notification", "chat_id", chatID, "err", err)
	}
}

func formatEvent(e monitor.Event) string {
	title := html.EscapeString(e.Title)
	url := html.EscapeString(e.URL)
	switch e.Source {
	case "axs":
		return fmt.Sprintf(
			"🚨 <b>билеты на TI 2026</b>\n\n"+
				"%s\n\n"+
				"<a href=\"%s\">купить на AXS →</a>",
			title, url,
		)
	case "steam":
		return fmt.Sprintf(
			"🚨 <b>анонс — Valve</b>\n\n"+
				"%s\n\n"+
				"<a href=\"%s\">читать →</a>",
			title, url,
		)
	case "reddit":
		return fmt.Sprintf(
			"🚨 <b>r/DotA2</b>\n\n"+
				"%s\n\n"+
				"<a href=\"%s\">открыть →</a>",
			title, url,
		)
	default:
		return fmt.Sprintf("▸ <b>%s</b>\n%s", title, url)
	}
}
