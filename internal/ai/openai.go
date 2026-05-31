package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

type openaiClient struct {
	cfg                 Config
	cache               Cache
	consecutiveFailures atomic.Int32
	disabled            atomic.Bool
	disabledAtNanos     atomic.Int64
	hardDisabled        atomic.Bool
	notified            atomic.Bool
	disableStreak       atomic.Int32
}

const (
	cooldownBase = 30 * time.Minute
	cooldownMax  = 6 * time.Hour
)

func cooldownFor(streak int32) time.Duration {
	d := cooldownBase
	for i := int32(1); i < streak; i++ {
		d *= 2
		if d >= cooldownMax {
			return cooldownMax
		}
	}
	return d
}

func (c *openaiClient) IsEnabled() bool {
	if !c.disabled.Load() {
		return true
	}
	if c.hardDisabled.Load() {
		return false
	}
	since := time.Since(time.Unix(0, c.disabledAtNanos.Load()))
	if since < cooldownFor(c.disableStreak.Load()) {
		return false
	}
	c.consecutiveFailures.Store(0)
	c.disabled.Store(false)
	slog.Info("ai: cooldown passed, re-enabled", "after", since.Round(time.Second))
	return true
}

func (c *openaiClient) ModelFast() string  { return c.cfg.ModelFast }
func (c *openaiClient) ModelSmart() string { return c.cfg.ModelSmart }

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiResponseFormat struct {
	Type string `json:"type"`
}

type oaiRequest struct {
	Model          string             `json:"model"`
	Messages       []oaiMessage       `json:"messages"`
	MaxTokens      int                `json:"max_tokens,omitempty"`
	Temperature    float64            `json:"temperature"`
	ResponseFormat *oaiResponseFormat `json:"response_format,omitempty"`
}

type oaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type oaiChoice struct {
	Message oaiMessage `json:"message"`
}

type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
	Usage   oaiUsage    `json:"usage"`
	Error   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

const consecutiveFailuresLimit = 5

func (c *openaiClient) Complete(ctx context.Context, req CompleteRequest) (*CompleteResponse, error) {
	if !c.IsEnabled() {
		return nil, ErrAINotEnabled
	}
	if req.Model == "" {
		req.Model = c.cfg.ModelFast
	}

	cacheKey := req.CacheKey
	if cacheKey == "" {
		cacheKey = HashKey(req.Model, req.System, req.User)
	}
	if cached, ok := c.cache.Get(cacheKey); ok {
		var cr CompleteResponse
		if err := json.Unmarshal(cached, &cr); err == nil {
			cr.Usage.Cached = true
			cr.Latency = 0
			return &cr, nil
		}
	}

	start := time.Now()
	resp, err := c.do(ctx, req)
	if err != nil {
		fails := c.consecutiveFailures.Add(1)
		slog.Warn("ai: request failed", "err", err, "fails", fails, "model", req.Model)
		hardFatal := errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrInsufficientQuota)
		if hardFatal || fails >= consecutiveFailuresLimit {
			c.disabled.Store(true)
			c.disabledAtNanos.Store(time.Now().UnixNano())
			streak := c.disableStreak.Add(1)
			if hardFatal {
				c.hardDisabled.Store(true)
			}
			if !c.notified.Swap(true) {
				slog.Warn("ai: disabled", "fails", fails, "hard", hardFatal, "err", err)
				if c.cfg.OnFailureRun != nil {
					c.cfg.OnFailureRun(err)
				}
			} else {
				slog.Warn("ai: re-disabled, alert suppressed", "streak", streak, "cooldown", cooldownFor(streak), "err", err)
			}
		}
		return nil, err
	}
	if c.disabled.Load() {
		c.disabled.Store(false)
		c.notified.Store(false)
		c.disableStreak.Store(0)
		slog.Info("ai: recovered after success")
	}
	c.consecutiveFailures.Store(0)
	resp.Latency = time.Since(start)

	if data, jerr := json.Marshal(resp); jerr == nil {
		_ = c.cache.Set(cacheKey, data, c.cfg.CacheTTL)
	}
	return resp, nil
}

func (c *openaiClient) do(ctx context.Context, req CompleteRequest) (*CompleteResponse, error) {
	messages := []oaiMessage{}
	if req.System != "" {
		messages = append(messages, oaiMessage{Role: "system", Content: req.System})
	}
	messages = append(messages, oaiMessage{Role: "user", Content: req.User})

	payload := oaiRequest{
		Model:       req.Model,
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: 0.1,
	}
	if req.JSONMode {
		payload.ResponseFormat = &oaiResponseFormat{Type: "json_object"}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
		resp, err := c.attempt(ctx, body)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if err == ErrUnauthorized {
			break
		}
	}
	return nil, lastErr
}

func (c *openaiClient) attempt(ctx context.Context, body []byte) (*CompleteResponse, error) {
	rctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(rctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new req: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		if rctx.Err() == context.DeadlineExceeded {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("http: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 5<<20))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	if httpResp.StatusCode == 401 || httpResp.StatusCode == 403 {
		return nil, ErrUnauthorized
	}
	if httpResp.StatusCode == 429 {
		var probe struct {
			Error struct {
				Code string `json:"code"`
				Type string `json:"type"`
			} `json:"error"`
		}
		_ = json.Unmarshal(respBody, &probe)
		if probe.Error.Code == "insufficient_quota" || probe.Error.Type == "insufficient_quota" {
			return nil, ErrInsufficientQuota
		}
		return nil, ErrRateLimit
	}
	if httpResp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %d: %s", httpResp.StatusCode, truncate(string(respBody), 300))
	}

	var oai oaiResponse
	if err := json.Unmarshal(respBody, &oai); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadResponse, err)
	}
	if oai.Error != nil {
		return nil, fmt.Errorf("%w: %s", ErrBadResponse, oai.Error.Message)
	}
	if len(oai.Choices) == 0 {
		return nil, fmt.Errorf("%w: no choices", ErrBadResponse)
	}

	return &CompleteResponse{
		Content: oai.Choices[0].Message.Content,
		Usage: Usage{
			PromptTokens:     oai.Usage.PromptTokens,
			CompletionTokens: oai.Usage.CompletionTokens,
			TotalTokens:      oai.Usage.TotalTokens,
			CostUSD:          calcCostUSD(payloadModel(body), oai.Usage),
		},
	}, nil
}

func payloadModel(body []byte) string {
	var p struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &p)
	return p.Model
}

func calcCostUSD(model string, u oaiUsage) float64 {
	in, out := pricePerMTokens(model)
	return float64(u.PromptTokens)*in/1_000_000 + float64(u.CompletionTokens)*out/1_000_000
}

func pricePerMTokens(model string) (in, out float64) {
	switch model {
	case "gpt-4o-mini":
		return 0.15, 0.60
	case "gpt-4o":
		return 2.50, 10.00
	case "gpt-4.1-mini":
		return 0.40, 1.60
	case "gpt-4.1":
		return 2.00, 8.00
	default:
		return 0.15, 0.60
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
