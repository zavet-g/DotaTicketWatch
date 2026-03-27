package notifier

import (
	"fmt"
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
	return n.NotifyText(formatEvent(event))
}

func (n *TelegramNotifier) NotifyText(text string) error {
	subs, err := n.storage.AllSubscribers()
	if err != nil {
		return fmt.Errorf("load subscribers: %w", err)
	}
	for _, sub := range subs {
		msg := tgbotapi.NewMessage(sub.ChatID, text)
		msg.ParseMode = tgbotapi.ModeHTML
		msg.DisableWebPagePreview = true
		if _, err := n.bot.Send(msg); err != nil {
			slog.Warn("failed to send notification", "chat_id", sub.ChatID, "err", err)
		}
	}
	return nil
}

func formatEvent(e monitor.Event) string {
	switch e.Source {
	case "axs":
		return fmt.Sprintf(
			"🚨 <b>билеты на TI 2026</b>\n\n"+
				"%s\n\n"+
				"<a href=\"%s\">купить на AXS →</a>",
			e.Title, e.URL,
		)
	case "steam":
		return fmt.Sprintf(
			"🚨 <b>анонс — Valve</b>\n\n"+
				"%s\n\n"+
				"<a href=\"%s\">читать →</a>",
			e.Title, e.URL,
		)
	case "reddit":
		return fmt.Sprintf(
			"🚨 <b>r/DotA2</b>\n\n"+
				"%s\n\n"+
				"<a href=\"%s\">открыть →</a>",
			e.Title, e.URL,
		)
	default:
		return fmt.Sprintf("▸ <b>%s</b>\n%s", e.Title, e.URL)
	}
}
