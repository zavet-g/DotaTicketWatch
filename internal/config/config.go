package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramToken   string
	AdminChatID     int64
	PollIntervalMin int
	AXSHubURL       string
	SteamNewsURL    string
	FlareSolverrURL string
	LogLevel        string
	DBPath          string

	TravelpayoutsToken  string
	TravelpayoutsMarker string
	FlightsOrigins      []string
	FlightsPollMin      int

	OpenAIKey         string
	OpenAIBaseURL     string
	OpenAIModelFast   string
	OpenAIModelSmart  string
	OpenAITimeoutSec  int
	OpenAIMaxRetries  int
	AICacheTTLHours   int

	CNMonitorEnabled bool
	CNNewsURL        string
	AXSDiffEnabled   bool

	MonitorEnabled bool
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		TelegramToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
		AXSHubURL:       getEnvOrDefault("AXS_HUB_URL", "https://www.axs.com/teams/1119906/the-international-dota-2-tickets"),
		SteamNewsURL:    getEnvOrDefault("STEAM_NEWS_URL", "https://api.steampowered.com/ISteamNews/GetNewsForApp/v0002/?appid=570&count=20&format=json"),
		FlareSolverrURL: getEnvOrDefault("FLARESOLVERR_URL", "http://localhost:8191"),
		LogLevel:        getEnvOrDefault("LOG_LEVEL", "info"),
		DBPath:          getEnvOrDefault("DB_PATH", "./data/bot.db"),
	}
	if idStr := os.Getenv("ADMIN_CHAT_ID"); idStr != "" {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			cfg.AdminChatID = id
		}
	}

	intervalStr := getEnvOrDefault("POLL_INTERVAL_MINUTES", "5")
	interval, err := strconv.Atoi(intervalStr)
	if err != nil || interval < 2 {
		return nil, fmt.Errorf("POLL_INTERVAL_MINUTES must be >= 2, got %q", intervalStr)
	}
	cfg.PollIntervalMin = interval

	cfg.TravelpayoutsToken = os.Getenv("TRAVELPAYOUTS_TOKEN")
	cfg.TravelpayoutsMarker = os.Getenv("TRAVELPAYOUTS_MARKER")
	if origins := os.Getenv("FLIGHTS_ORIGINS"); origins != "" {
		for _, o := range strings.Split(origins, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				cfg.FlightsOrigins = append(cfg.FlightsOrigins, trimmed)
			}
		}
	}
	flightsPollStr := getEnvOrDefault("FLIGHTS_POLL_INTERVAL_MIN", "30")
	cfg.FlightsPollMin, _ = strconv.Atoi(flightsPollStr)
	if cfg.FlightsPollMin < 10 {
		cfg.FlightsPollMin = 30
	}

	cfg.OpenAIKey = os.Getenv("OPENAI_API_KEY")
	cfg.OpenAIBaseURL = getEnvOrDefault("OPENAI_BASE_URL", "https://api.openai.com/v1")
	cfg.OpenAIModelFast = getEnvOrDefault("OPENAI_MODEL_FAST", "gpt-4o-mini")
	cfg.OpenAIModelSmart = getEnvOrDefault("OPENAI_MODEL_SMART", "gpt-4o")
	cfg.OpenAITimeoutSec, _ = strconv.Atoi(getEnvOrDefault("OPENAI_TIMEOUT_S", "20"))
	if cfg.OpenAITimeoutSec < 5 {
		cfg.OpenAITimeoutSec = 20
	}
	cfg.OpenAIMaxRetries, _ = strconv.Atoi(getEnvOrDefault("OPENAI_MAX_RETRIES", "1"))
	cfg.AICacheTTLHours, _ = strconv.Atoi(getEnvOrDefault("AI_CACHE_TTL_HOURS", "720"))

	cfg.CNMonitorEnabled = strings.EqualFold(os.Getenv("CN_MONITOR_ENABLED"), "true")
	cfg.CNNewsURL = getEnvOrDefault("CN_NEWS_URL", "https://www.dota2.com.cn/news/index.htm")
	cfg.AXSDiffEnabled = strings.EqualFold(os.Getenv("AXS_DIFF_ENABLED"), "true")
	cfg.MonitorEnabled = !strings.EqualFold(os.Getenv("MONITOR_ENABLED"), "false")

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	var missing []string
	if strings.TrimSpace(c.TelegramToken) == "" {
		missing = append(missing, "TELEGRAM_BOT_TOKEN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	return nil
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
