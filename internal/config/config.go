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
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		TelegramToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
		AXSHubURL:       getEnvOrDefault("AXS_HUB_URL", "https://www.axs.com/teams/1119906/the-international-dota-2-tickets"),
		SteamNewsURL:    getEnvOrDefault("STEAM_NEWS_URL", "https://api.steampowered.com/ISteamNews/GetNewsForApp/v0002/?appid=570&count=10&format=json"),
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
