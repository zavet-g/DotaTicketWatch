package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/artem/dotaticketwatch/internal/config"
	"github.com/artem/dotaticketwatch/internal/fetcher"
	"github.com/artem/dotaticketwatch/internal/flights"
	"github.com/artem/dotaticketwatch/internal/monitor"
	"github.com/artem/dotaticketwatch/internal/notifier"
	"github.com/artem/dotaticketwatch/internal/storage"
	"github.com/artem/dotaticketwatch/internal/weather"
	"github.com/artem/dotaticketwatch/internal/yuan"
)

const (
	checkCooldown       = 60 * time.Second
	flightRateWindow    = 3 * time.Hour
	flightRateMax       = 50
	flightPendingExpiry = 60 * time.Second
)

//go:embed faq_beacon.mp4
var faqBeaconMP4 []byte

//go:embed flight_arc.mp4
var flightArcMP4 []byte

//go:embed yuan_chart.mp4
var yuanChartMP4 []byte

//go:embed weather_sky.mp4
var weatherSkyMP4 []byte

type pendingDateReq struct {
	messageID   int
	promptMsgID int
	createdAt   time.Time
}

type flightState struct {
	mu       sync.Mutex
	pending  map[int64]*pendingDateReq
	rateHits map[int64][]time.Time
}

func newFlightState() *flightState {
	return &flightState{
		pending:  make(map[int64]*pendingDateReq),
		rateHits: make(map[int64][]time.Time),
	}
}

func (fs *flightState) setPending(chatID int64, msgID, promptMsgID int) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.pending[chatID] = &pendingDateReq{messageID: msgID, promptMsgID: promptMsgID, createdAt: time.Now()}
}

func (fs *flightState) getPending(chatID int64) (int, int, bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	p, ok := fs.pending[chatID]
	if !ok {
		return 0, 0, false
	}
	if time.Since(p.createdAt) > flightPendingExpiry {
		delete(fs.pending, chatID)
		return 0, 0, false
	}
	return p.messageID, p.promptMsgID, true
}

func (fs *flightState) clearPending(chatID int64) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	delete(fs.pending, chatID)
}

func (fs *flightState) canRate(chatID int64) (bool, time.Duration) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-flightRateWindow)
	var valid []time.Time
	for _, t := range fs.rateHits[chatID] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	if len(valid) == 0 {
		delete(fs.rateHits, chatID)
	} else {
		fs.rateHits[chatID] = valid
	}
	if len(valid) >= flightRateMax {
		remaining := valid[0].Add(flightRateWindow).Sub(now)
		return false, remaining
	}
	return true, 0
}

func (fs *flightState) consumeRate(chatID int64) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.rateHits[chatID] = append(fs.rateHits[chatID], time.Now())
}

func (fs *flightState) cleanup() {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	cutoff := time.Now().Add(-flightRateWindow)
	for chatID, hits := range fs.rateHits {
		var valid []time.Time
		for _, t := range hits {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(fs.rateHits, chatID)
		} else {
			fs.rateHits[chatID] = valid
		}
	}
	for chatID, p := range fs.pending {
		if time.Since(p.createdAt) > flightPendingExpiry {
			delete(fs.pending, chatID)
		}
	}
}

var moscow = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("MSK", 3*60*60)
	}
	return loc
}()

type monitorStat struct {
	lastCheckAt    time.Time
	lastErr        error
	lastErrText    string
	lastCount      int
	consecutiveFails int
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

const adminRenotifyEvery = 10

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
	changed := newErrText != st.lastErrText
	if newErrText != "" {
		if changed {
			st.consecutiveFails = 1
		} else {
			st.consecutiveFails++
		}
		shouldNotify = changed || st.consecutiveFails%adminRenotifyEvery == 0
	} else {
		st.consecutiveFails = 0
		shouldNotify = changed
	}
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

	cleanScopes := []tgbotapi.BotCommandScope{
		tgbotapi.NewBotCommandScopeDefault(),
		tgbotapi.NewBotCommandScopeAllPrivateChats(),
	}
	if cfg.AdminChatID > 0 {
		cleanScopes = append(cleanScopes, tgbotapi.NewBotCommandScopeChat(cfg.AdminChatID))
	}
	for _, scope := range cleanScopes {
		del := tgbotapi.NewDeleteMyCommandsWithScope(scope)
		bot.Request(del)
	}

	userCommands := []tgbotapi.BotCommand{
		{Command: "start", Description: "подписаться"},
		{Command: "stop", Description: "отписаться"},
		{Command: "flight", Description: "перелёт в шанхай"},
		{Command: "yuan", Description: "курс юаня"},
		{Command: "weather", Description: "погода в шанхае"},
		{Command: "faq", Description: "познать истину"},
	}
	for _, scope := range []tgbotapi.BotCommandScope{
		tgbotapi.NewBotCommandScopeDefault(),
		tgbotapi.NewBotCommandScopeAllPrivateChats(),
	} {
		cmd := tgbotapi.NewSetMyCommandsWithScope(scope, userCommands...)
		if _, err := bot.Request(cmd); err != nil {
			slog.Error("failed to set commands", "scope", scope.Type, "err", err)
		}
	}

	if cfg.AdminChatID > 0 {
		adminCommands := append(userCommands,
			tgbotapi.BotCommand{Command: "check", Description: "проверить вручную"},
			tgbotapi.BotCommand{Command: "status", Description: "статус системы"},
		)
		scope := tgbotapi.NewBotCommandScopeChat(cfg.AdminChatID)
		cmd := tgbotapi.NewSetMyCommandsWithScope(scope, adminCommands...)
		if _, err := bot.Request(cmd); err != nil {
			slog.Error("failed to set admin commands", "err", err)
		}
	}

	ntf := notifier.NewTelegramNotifier(bot, store)
	monitors := []monitor.Monitor{
		monitor.NewSteamNewsMonitor(cfg.SteamNewsURL),
		monitor.NewAXSMonitor(cfg.AXSHubURL, cfg.FlareSolverrURL, fetcher.Fetch),
		monitor.NewRedditMonitor(),
	}

	st := newAppState()
	fst := newFlightState()

	var flightsCache *flights.Cache
	if cfg.TravelpayoutsToken != "" && len(cfg.FlightsOrigins) > 0 {
		fc := flights.NewClient(cfg.TravelpayoutsToken, cfg.TravelpayoutsMarker)
		flightsCache = flights.NewCache(fc, cfg.FlightsOrigins, cfg.FlightsPollMin)
	}

	yuanCache := yuan.NewCache(moscow)
	weatherCache := weather.NewCache()

	adminFn := func(text string) {
		if cfg.AdminChatID == 0 {
			return
		}
		sendDirect(bot, cfg.AdminChatID, text)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if flightsCache != nil {
		go flightsCache.Start(ctx)
	}
	go yuanCache.Start(ctx)
	go weatherCache.Start(ctx)

	var checkMu sync.Mutex
	var lastCheck sync.Map

	go runPolling(ctx, cfg, monitors, ntf, store, st, adminFn, &checkMu)
	go runBotCommands(ctx, bot, cfg, store, monitors, ntf, st, adminFn, &checkMu, &lastCheck, flightsCache, fst, yuanCache, weatherCache)
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fst.cleanup()
			}
		}
	}()

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
				adminFn(fmt.Sprintf("▸ <b>%s</b> · найдено событие\n<code>%s</code>\n%s", m.Name(), event.ID, event.Title))
				var notified bool
				for attempt := range 3 {
					if err2 := ntf.Notify(event); err2 != nil {
						slog.Error("notify failed", "event_id", event.ID, "attempt", attempt+1, "err", err2)
						adminFn(fmt.Sprintf("× отправка не прошла · попытка %d/3\n<code>%v</code>", attempt+1, err2))
						time.Sleep(5 * time.Second)
						continue
					}
					notified = true
					break
				}
				if !notified {
					adminFn(fmt.Sprintf("🚨 <b>КРИТИЧНО</b> · событие %s не доставлено после 3 попыток", event.ID))
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
	flightsCache *flights.Cache,
	fst *flightState,
	yuanCache *yuan.Cache,
	weatherCache *weather.Cache,
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
			if update.CallbackQuery != nil {
				handleCallback(update.CallbackQuery, bot, flightsCache, fst, cfg.AdminChatID)
				continue
			}
			if update.Message == nil {
				continue
			}
			if update.Message.IsCommand() {
				fst.clearPending(update.Message.Chat.ID)
				handleCommand(update.Message, bot, cfg, store, monitors, ntf, st, adminFn, checkMu, lastCheck, flightsCache, yuanCache, weatherCache)
				continue
			}
			go handleText(update.Message, bot, flightsCache, fst)
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
	flightsCache *flights.Cache,
	yuanCache *yuan.Cache,
	weatherCache *weather.Cache,
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
				text = "· подписан\n\n<i>уведомлю когда появятся билеты на TI 2026</i>\n\n▸ /flight — <i>рейс до шанхая уже подобран</i>"
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
		text = buildStatusText(cfg, store, monitors, st, flightsCache, yuanCache, weatherCache)

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

	case "flight":
		go sendFlight(bot, chatID, flightsCache)
		return

	case "yuan":
		go sendYuan(bot, chatID, yuanCache)
		return

	case "weather":
		go sendWeather(bot, chatID, weatherCache)
		return

	case "faq":
		go sendFaq(bot, chatID)
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

func buildStatusText(cfg *config.Config, store *storage.Storage, monitors []monitor.Monitor, st *appState, flightsCache *flights.Cache, yuanCache *yuan.Cache, weatherCache *weather.Cache) string {
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
	if axsStat, ok := st.get("AXS"); ok && axsStat.lastErr == nil {
		sb.WriteString("· FlareSolverr\n")
	} else {
		sb.WriteString("× FlareSolverr — недоступен\n")
	}
	if info, err := os.Stat(cfg.DBPath); err == nil {
		sb.WriteString(fmt.Sprintf("· база данных — %s\n", formatBytes(info.Size())))
	}

	if flightsCache != nil {
		sb.WriteString("\n<b>авиабилеты</b>\n")
		snap := flightsCache.Get()
		if snap == nil {
			sb.WriteString("  кэш не загружен\n")
		} else {
			dep, _ := time.Parse("2006-01-02", snap.Departure)
			ret, _ := time.Parse("2006-01-02", snap.Return)
			ago := time.Since(snap.UpdatedAt).Round(time.Second)
			sb.WriteString(fmt.Sprintf("· %s → %s · обновлено %s назад\n",
				formatDateRu(dep), formatDateRu(ret), ago))
			for _, r := range snap.Routes {
				if r.Price > 0 {
					sb.WriteString(fmt.Sprintf("  %s → %s · %s ₽ · %s\n",
						r.OriginAirport, r.DestinationAirport, formatPrice(r.Price), r.AirlineName))
				} else {
					sb.WriteString(fmt.Sprintf("  %s → SHA · нет данных\n", r.Origin))
				}
			}
			nextIn := time.Duration(cfg.FlightsPollMin)*time.Minute - time.Since(snap.UpdatedAt)
			if nextIn < 0 {
				nextIn = 0
			}
			sb.WriteString(fmt.Sprintf("· опрос каждые %d мин · следующий через %s\n", cfg.FlightsPollMin, nextIn.Round(time.Second)))
		}
	}

	sb.WriteString("\n<b>юань</b>\n")
	if yuanSnap := yuanCache.Get(); yuanSnap == nil {
		sb.WriteString("  кэш не загружен\n")
	} else {
		price := yuanSnap.Rate.Last
		if !yuanSnap.Rate.Trading || price == 0 {
			price = yuanSnap.Rate.PrevPrice
		}
		ago := time.Since(yuanSnap.UpdatedAt).Round(time.Second)
		sb.WriteString(fmt.Sprintf("· MOEX CNYRUB_TOM — %.2f · обновлено %s назад\n", price, ago))
	}

	sb.WriteString("\n<b>погода</b>\n")
	if weatherSnap := weatherCache.Get(); weatherSnap == nil {
		sb.WriteString("  кэш не загружен\n")
	} else {
		ago := time.Since(weatherSnap.UpdatedAt).Round(time.Second)
		sb.WriteString(fmt.Sprintf("· open-meteo — %d дней · обновлено %s назад\n", len(weatherSnap.Days), ago))
	}

	sb.WriteString(fmt.Sprintf("\nподписчиков <b>%d</b>  ·  уведомлений <b>%d</b>\n",
		store.SubscriberCount(), store.NotifiedCount()))

	return sb.String()
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

func sendFlight(bot *tgbotapi.BotAPI, chatID int64, cache *flights.Cache) {
	if cache == nil {
		reply(bot, chatID, "× перелёты недоступны")
		return
	}
	snap := cache.Get()
	parts := formatFlightParts(snap, moscow, false)
	if len(parts) == 0 {
		reply(bot, chatID, "× нет данных · попробуй позже")
		return
	}

	anim := tgbotapi.NewAnimation(chatID, tgbotapi.FileBytes{Name: "flight.mp4", Bytes: flightArcMP4})
	anim.Caption = parts[0]
	anim.ParseMode = tgbotapi.ModeHTML
	sent, err := bot.Send(anim)
	if err != nil {
		slog.Warn("sendFlight failed", "chat_id", chatID, "err", err)
		return
	}

	caption := parts[0]
	for i := 1; i < len(parts); i++ {
		time.Sleep(600 * time.Millisecond)
		caption += "\n" + parts[i]
		isLast := i == len(parts)-1

		edit := tgbotapi.NewEditMessageCaption(chatID, sent.MessageID, caption)
		edit.ParseMode = tgbotapi.ModeHTML
		if isLast {
			btn := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("изменить даты", "flight_dates"),
				),
			)
			edit.ReplyMarkup = &btn
		}
		bot.Send(edit)
	}
}

func editFlightProgressive(bot *tgbotapi.BotAPI, chatID int64, msgID int, parts []string) {
	if len(parts) == 0 {
		return
	}

	caption := parts[0]
	edit := tgbotapi.NewEditMessageCaption(chatID, msgID, caption)
	edit.ParseMode = tgbotapi.ModeHTML
	bot.Send(edit)

	for i := 1; i < len(parts); i++ {
		time.Sleep(600 * time.Millisecond)
		caption += "\n" + parts[i]
		isLast := i == len(parts)-1

		edit := tgbotapi.NewEditMessageCaption(chatID, msgID, caption)
		edit.ParseMode = tgbotapi.ModeHTML
		if isLast {
			btn := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("изменить даты", "flight_dates"),
				),
			)
			edit.ReplyMarkup = &btn
		}
		bot.Send(edit)
	}
}

func handleCallback(cq *tgbotapi.CallbackQuery, bot *tgbotapi.BotAPI, cache *flights.Cache, fst *flightState, adminChatID int64) {
	if cq.Data != "flight_dates" || cache == nil {
		return
	}
	chatID := cq.Message.Chat.ID
	userID := cq.From.ID
	origMsgID := cq.Message.MessageID

	callback := tgbotapi.NewCallback(cq.ID, "")
	bot.Request(callback)

	isAdmin := adminChatID > 0 && chatID == adminChatID
	if !isAdmin {
		ok, remaining := fst.canRate(userID)
		if !ok {
			mins := int(remaining.Minutes()) + 1
			reply(bot, chatID, fmt.Sprintf("× лимит · осталось %d мин", mins))
			return
		}
	}

	if _, oldPrompt, hasPending := fst.getPending(chatID); hasPending {
		deleteMsg(bot, chatID, oldPrompt)
	}

	sent, err := bot.Send(newHTMLMessage(chatID, "<i>введи даты вылета и возврата</i>\nформат: <code>12.08 24.08</code>"))
	if err != nil {
		return
	}
	fst.setPending(chatID, origMsgID, sent.MessageID)
}

func handleText(msg *tgbotapi.Message, bot *tgbotapi.BotAPI, cache *flights.Cache, fst *flightState) {
	chatID := msg.Chat.ID
	userID := msg.From.ID
	origMsgID, promptMsgID, ok := fst.getPending(chatID)
	if !ok || cache == nil {
		return
	}
	fst.clearPending(chatID)

	dateParts := strings.Fields(msg.Text)
	if len(dateParts) != 2 {
		reply(bot, chatID, "× не понял · формат: <code>12.08 24.08</code>")
		return
	}

	departure, err1 := parseDateInput(dateParts[0])
	ret, err2 := parseDateInput(dateParts[1])
	if err1 != nil || err2 != nil {
		reply(bot, chatID, "× не понял · формат: <code>12.08 24.08</code>")
		return
	}

	fst.consumeRate(userID)

	deleteMsg(bot, chatID, msg.MessageID)
	deleteMsg(bot, chatID, promptMsgID)

	snap := cache.FetchCustom(departure, ret)
	parts := formatFlightParts(snap, moscow, true)
	editFlightProgressive(bot, chatID, origMsgID, parts)
}

func parseDateInput(s string) (string, error) {
	t, err := time.Parse("02.01", s)
	if err != nil {
		return "", err
	}
	day, month := t.Day(), int(t.Month())
	check := time.Date(2026, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if check.Day() != day || check.Month() != time.Month(month) {
		return "", fmt.Errorf("invalid date")
	}
	return fmt.Sprintf("2026-%02d-%02d", month, day), nil
}

func deleteMsg(bot *tgbotapi.BotAPI, chatID int64, msgID int) {
	del := tgbotapi.NewDeleteMessage(chatID, msgID)
	bot.Request(del)
}

func sendFaq(bot *tgbotapi.BotAPI, chatID int64) {
	parts := []string{
		"<b>зачем это</b>\n\n" +
			"<code>TI 2026 · шанхай · август</code>",

		"ты хочешь быть в зале когда поднимут аегис.\n" +
			"<i>не на стриме. не в пересказе. не в клипах на следующий день.</i>\n" +
			"<b>в зале.</b>",

		"билеты появятся один раз и сгорят за минуты.\n" +
			"<i>ты в этот момент будешь спать. или работать. или жить.</i>",

		"<b>а этот бот — нет.</b>\n" +
			"он смотрит на AXS каждые пять минут.\n" +
			"читает Valve раньше реддита.\n" +
			"видит очередь до того как она дойдёт до тебя.",

		"▸ когда ворота в шанхай откроются — здесь загорится свет.\n" +
			"<b>ты увидишь его первым.</b>",

		"но билет не дарят — <b>его берут</b>.\n" +
			"<b>этот бой — твой.</b>",

		"а между тобой и залом — дуга над облаками.\n" +
			"прямой борт. курс на восток.\n" +
			"/flight — <b>решайся.</b>",

		"рейс есть. но шанхай на юанях.\n" +
			"/yuan — <b>знай курс.</b>",

		"а небо над ареной не спрашивает.\n" +
			"/weather — <b>смотри заранее.</b>",

		"<blockquote><i>нас мало. сюда не приходят случайно.</i>\n" +
			"этот бот — <b>маяк</b>. он горит для своих.</blockquote>",
	}

	anim := tgbotapi.NewAnimation(chatID, tgbotapi.FileBytes{Name: "faq.mp4", Bytes: faqBeaconMP4})
	anim.Caption = parts[0]
	anim.ParseMode = tgbotapi.ModeHTML
	initMsg, err := bot.Send(anim)
	if err != nil {
		return
	}

	caption := parts[0]
	for i := 1; i < len(parts); i++ {
		time.Sleep(time.Second)
		caption += "\n\n" + parts[i]
		editCaption(bot, chatID, initMsg.MessageID, caption)
	}
}

func editCaption(bot *tgbotapi.BotAPI, chatID int64, msgID int, caption string) {
	edit := tgbotapi.NewEditMessageCaption(chatID, msgID, caption)
	edit.ParseMode = tgbotapi.ModeHTML
	_, _ = bot.Send(edit)
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
