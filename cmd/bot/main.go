package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/artem/dotaticketwatch/internal/config"
	"github.com/artem/dotaticketwatch/internal/fetcher"
	"github.com/artem/dotaticketwatch/internal/monitor"
	"github.com/artem/dotaticketwatch/internal/notifier"
	"github.com/artem/dotaticketwatch/internal/storage"
)

const checkCooldown = 60 * time.Second

var moscow = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("MSK", 3*60*60)
	}
	return loc
}()

type monitorStat struct {
	lastCheckAt time.Time
	lastErr     error
	lastErrText string
	lastCount   int
}

type appState struct {
	startTime   time.Time
	mu          sync.RWMutex
	stats       map[string]*monitorStat
	announcedAt time.Time
}

func newAppState() *appState {
	return &appState{
		startTime: time.Now(),
		stats:     make(map[string]*monitorStat),
	}
}

func (s *appState) updateAndCheck(name string, err error, count int) (shouldNotify, wasError bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stats[name]
	if st == nil {
		st = &monitorStat{}
		s.stats[name] = st
	}
	newErrText := ""
	if err != nil {
		newErrText = err.Error()
	}
	wasError = st.lastErrText != ""
	shouldNotify = newErrText != st.lastErrText
	st.lastCheckAt = time.Now()
	st.lastErr = err
	st.lastErrText = newErrText
	st.lastCount = count
	return
}

func (s *appState) setAnnounced() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	first := s.announcedAt.IsZero() || time.Since(s.announcedAt) >= 72*time.Hour
	s.announcedAt = time.Now()
	return first
}

func (s *appState) isAccelerated() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.announcedAt.IsZero() && time.Since(s.announcedAt) < 72*time.Hour
}

func (s *appState) get(name string) (monitorStat, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.stats[name]
	if !ok {
		return monitorStat{}, false
	}
	return *st, true
}

type checkResult struct {
	err       error
	newEvents int
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}
	setupLogger(cfg.LogLevel)

	store, err := storage.New(cfg.DBPath)
	if err != nil {
		slog.Error("storage init failed", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	bot, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		slog.Error("telegram bot init failed", "err", err)
		os.Exit(1)
	}
	slog.Info("bot started", "username", bot.Self.UserName)

	ntf := notifier.NewTelegramNotifier(bot, store)
	monitors := []monitor.Monitor{
		monitor.NewSteamNewsMonitor(cfg.SteamNewsURL),
		monitor.NewAXSMonitor(cfg.AXSHubURL, cfg.FlareSolverrURL, fetcher.Fetch),
		monitor.NewRedditMonitor(),
	}

	st := newAppState()

	adminFn := func(text string) {
		if cfg.AdminChatID == 0 {
			return
		}
		sendDirect(bot, cfg.AdminChatID, text)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var checkMu sync.Mutex
	var lastCheck sync.Map

	go runPolling(ctx, cfg, monitors, ntf, store, st, adminFn, &checkMu)
	go runBotCommands(ctx, bot, cfg, store, monitors, ntf, st, adminFn, &checkMu, &lastCheck)

	<-ctx.Done()
	slog.Info("shutting down")
}

func runChecks(
	monitors []monitor.Monitor,
	ntf *notifier.TelegramNotifier,
	store *storage.Storage,
	st *appState,
	adminFn func(string),
	onDone func(name string, res checkResult),
) {
	for _, m := range monitors {
		events, err := m.Check()
		shouldNotify, wasError := st.updateAndCheck(m.Name(), err, len(events))

		res := checkResult{err: err}

		if err != nil {
			slog.Warn("monitor check failed", "monitor", m.Name(), "err", err)
			if shouldNotify {
				adminFn(fmt.Sprintf("× <b>%s</b>\n<code>%v</code>", m.Name(), err))
			}
		} else {
			if shouldNotify && wasError {
				adminFn(fmt.Sprintf("· <b>%s</b> — восстановлен", m.Name()))
			}
			for _, event := range events {
				if store.AlreadyNotified(event.ID) {
					continue
				}
				if err2 := ntf.Notify(event); err2 != nil {
					slog.Error("notify failed", "event_id", event.ID, "err", err2)
					continue
				}
				if err2 := store.MarkNotified(event.ID); err2 != nil {
					slog.Error("mark notified failed", "event_id", event.ID, "err", err2)
				}
				slog.Info("notified", "source", event.Source, "event_id", event.ID)
				res.newEvents++
				if event.EventType == monitor.EventTypeAnnouncement {
					if st.setAnnounced() {
						adminFn("▸ режим ускоренного опроса — 1мин / 72ч")
					}
				}
			}
		}

		if onDone != nil {
			onDone(m.Name(), res)
		}
	}
}

func checkAll(
	monitors []monitor.Monitor,
	ntf *notifier.TelegramNotifier,
	store *storage.Storage,
	st *appState,
	adminFn func(string),
	mu *sync.Mutex,
) {
	mu.Lock()
	defer mu.Unlock()
	runChecks(monitors, ntf, store, st, adminFn, nil)
}

func runPolling(
	ctx context.Context,
	cfg *config.Config,
	monitors []monitor.Monitor,
	ntf *notifier.TelegramNotifier,
	store *storage.Storage,
	st *appState,
	adminFn func(string),
	mu *sync.Mutex,
) {
	base := time.Duration(cfg.PollIntervalMin) * time.Minute
	slog.Info("polling started", "base_interval", base)
	checkAll(monitors, ntf, store, st, adminFn, mu)

	for {
		interval := base
		if st.isAccelerated() {
			interval = time.Minute
		}
		jitter := time.Duration(rand.Int64N(int64(interval/2))) - interval/4
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval + jitter):
			slog.Debug("polling tick", "sleep", (interval + jitter).Round(time.Second))
			checkAll(monitors, ntf, store, st, adminFn, mu)
		}
	}
}

func runBotCommands(
	ctx context.Context,
	bot *tgbotapi.BotAPI,
	cfg *config.Config,
	store *storage.Storage,
	monitors []monitor.Monitor,
	ntf *notifier.TelegramNotifier,
	st *appState,
	adminFn func(string),
	checkMu *sync.Mutex,
	lastCheck *sync.Map,
) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := bot.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			bot.StopReceivingUpdates()
			return
		case update := <-updates:
			if update.Message == nil || !update.Message.IsCommand() {
				continue
			}
			handleCommand(update.Message, bot, cfg, store, monitors, ntf, st, adminFn, checkMu, lastCheck)
		}
	}
}

func handleCommand(
	msg *tgbotapi.Message,
	bot *tgbotapi.BotAPI,
	cfg *config.Config,
	store *storage.Storage,
	monitors []monitor.Monitor,
	ntf *notifier.TelegramNotifier,
	st *appState,
	adminFn func(string),
	checkMu *sync.Mutex,
	lastCheck *sync.Map,
) {
	chatID := msg.Chat.ID
	username := msg.From.UserName
	isAdmin := cfg.AdminChatID > 0 && chatID == cfg.AdminChatID

	var text string
	switch msg.Command() {
	case "start":
		if store.IsSubscribed(chatID) {
			text = "· уже подписан"
		} else {
			if err := store.AddSubscriber(chatID, username); err != nil {
				text = "× ошибка · попробуй позже"
			} else {
				text = "· подписан\n\n<i>уведомлю когда появятся билеты на TI 2026</i>"
			}
		}

	case "stop":
		if err := store.RemoveSubscriber(chatID); err != nil {
			text = "× ошибка · попробуй позже"
		} else {
			text = "· отписан"
		}

	case "status":
		if !isAdmin {
			return
		}
		text = buildStatusText(cfg, store, monitors, st)

	case "check":
		if !isAdmin {
			return
		}
		now := time.Now()
		if last, ok := lastCheck.Load(chatID); ok {
			if elapsed := now.Sub(last.(time.Time)); elapsed < checkCooldown {
				remaining := (checkCooldown - elapsed).Round(time.Second)
				reply(bot, chatID, fmt.Sprintf("подожди ещё %s", remaining))
				return
			}
		}
		lastCheck.Store(chatID, now)

		initMsg, err := bot.Send(newHTMLMessage(chatID, buildCheckText(monitors, nil)))
		if err != nil {
			return
		}
		msgID := initMsg.MessageID

		go func() {
			if !checkMu.TryLock() {
				editMessage(bot, chatID, msgID, "уже выполняется · повтори через 30с")
				return
			}
			defer checkMu.Unlock()

			results := make(map[string]checkResult)
			runChecks(monitors, ntf, store, st, adminFn, func(name string, res checkResult) {
				results[name] = res
				editMessage(bot, chatID, msgID, buildCheckText(monitors, results))
			})
		}()
		return

	default:
		return
	}

	reply(bot, chatID, text)
}

func buildCheckText(monitors []monitor.Monitor, results map[string]checkResult) string {
	allDone := results != nil && len(results) == len(monitors)
	var sb strings.Builder

	if !allDone {
		sb.WriteString("<i>проверяю</i>\n")
	} else {
		hasErr, hasNew := false, false
		for _, r := range results {
			if r.err != nil {
				hasErr = true
			}
			if r.newEvents > 0 {
				hasNew = true
			}
		}
		ts := time.Now().In(moscow).Format("02.01 · 15:04:05")
		switch {
		case hasNew:
			sb.WriteString(fmt.Sprintf("▸ <b>найдено</b>  <code>%s</code>\n", ts))
		case hasErr:
			sb.WriteString(fmt.Sprintf("× <b>ошибка</b>  <code>%s</code>\n", ts))
		default:
			sb.WriteString(fmt.Sprintf("· <b>готово</b>  <code>%s</code>\n", ts))
		}
	}

	sb.WriteString("\n")
	for _, m := range monitors {
		var res checkResult
		var done bool
		if results != nil {
			res, done = results[m.Name()]
		}
		switch {
		case !done:
			sb.WriteString(fmt.Sprintf("  %s\n", m.Name()))
		case res.err != nil:
			short := res.err.Error()
			if len(short) > 60 {
				short = short[:60] + "…"
			}
			sb.WriteString(fmt.Sprintf("× %s — <code>%s</code>\n", m.Name(), short))
		case res.newEvents > 0:
			sb.WriteString(fmt.Sprintf("▸ %s — %d новых · уведомления отправлены\n", m.Name(), res.newEvents))
		default:
			sb.WriteString(fmt.Sprintf("· %s — нет новых\n", m.Name()))
		}
	}

	return sb.String()
}

func buildStatusText(cfg *config.Config, store *storage.Storage, monitors []monitor.Monitor, st *appState) string {
	now := time.Now().In(moscow)
	uptime := time.Since(st.startTime).Round(time.Second)
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("<b>DotaTicketWatch</b>  <code>%s</code>\n", now.Format("02.01.2006 · 15:04")))
	sb.WriteString(fmt.Sprintf("<i>аптайм %s</i>\n", formatDuration(uptime)))

	sb.WriteString("\n<b>мониторы</b>\n")
	for _, m := range monitors {
		stat, ok := st.get(m.Name())
		if !ok {
			sb.WriteString(fmt.Sprintf("  %s\n", m.Name()))
		} else if stat.lastErr != nil {
			ago := time.Since(stat.lastCheckAt).Round(time.Second)
			short := stat.lastErr.Error()
			if len(short) > 55 {
				short = short[:55] + "…"
			}
			sb.WriteString(fmt.Sprintf("× %s — <code>%s</code> · %s назад\n", m.Name(), short, ago))
		} else {
			ago := time.Since(stat.lastCheckAt).Round(time.Second)
			sb.WriteString(fmt.Sprintf("· %s — %s назад\n", m.Name(), ago))
		}
	}

	if st.isAccelerated() {
		sb.WriteString("\n▸ ускоренный опрос — 1мин\n")
	}

	sb.WriteString("\n<b>инфраструктура</b>\n")
	if flareSolverrOK(cfg.FlareSolverrURL) {
		sb.WriteString("· FlareSolverr\n")
	} else {
		sb.WriteString("× FlareSolverr — недоступен\n")
	}
	if info, err := os.Stat(cfg.DBPath); err == nil {
		sb.WriteString(fmt.Sprintf("· база данных — %s\n", formatBytes(info.Size())))
	}

	sb.WriteString(fmt.Sprintf("\nподписчиков <b>%d</b>  ·  уведомлений <b>%d</b>\n",
		store.SubscriberCount(), store.NotifiedCount()))

	return sb.String()
}

func flareSolverrOK(url string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(url+"/v1", "application/json",
		strings.NewReader(`{"cmd":"sessions.list"}`))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dч %dм", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dм %dс", m, s)
	}
	return fmt.Sprintf("%dс", s)
}

func formatBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(b)/1024/1024)
}

func newHTMLMessage(chatID int64, text string) tgbotapi.MessageConfig {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.DisableWebPagePreview = true
	return msg
}

func reply(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := newHTMLMessage(chatID, text)
	if _, err := bot.Send(msg); err != nil {
		slog.Warn("reply failed", "chat_id", chatID, "err", err)
	}
}

func sendDirect(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := newHTMLMessage(chatID, text)
	if _, err := bot.Send(msg); err != nil {
		slog.Warn("sendDirect failed", "chat_id", chatID, "err", err)
	}
}

func editMessage(bot *tgbotapi.BotAPI, chatID int64, msgID int, text string) {
	edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
	edit.ParseMode = tgbotapi.ModeHTML
	edit.DisableWebPagePreview = true
	_, _ = bot.Send(edit)
}

func setupLogger(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().In(moscow).Format("2006-01-02T15:04:05-07:00"))
			}
			return a
		},
	})))
}
