package ai

import (
	"context"
	"strings"
	"time"
)

type CompleteRequest struct {
	Model     string
	System    string
	User      string
	JSONMode  bool
	MaxTokens int
	CacheKey  string
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CostUSD          float64
	Cached           bool
}

type CompleteResponse struct {
	Content string
	Usage   Usage
	Latency time.Duration
}

type Client interface {
	IsEnabled() bool
	ModelFast() string
	ModelSmart() string
	Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error)
}

type Config struct {
	APIKey       string
	BaseURL      string
	ModelFast    string
	ModelSmart   string
	Timeout      time.Duration
	MaxRetries   int
	CacheTTL     time.Duration
	OnFailureRun func(err error)
}

func NewClient(cfg Config, cache Cache) Client {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return &noopClient{}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.ModelFast == "" {
		cfg.ModelFast = "gpt-4o-mini"
	}
	if cfg.ModelSmart == "" {
		cfg.ModelSmart = "gpt-4o"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 720 * time.Hour
	}
	if cache == nil {
		cache = noopCache{}
	}
	return &openaiClient{cfg: cfg, cache: cache}
}
