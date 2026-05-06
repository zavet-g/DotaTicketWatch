package monitor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/artem/dotaticketwatch/internal/ai"
)

type SteamClassification struct {
	IsTicketSignal bool    `json:"is_ticket_signal"`
	Confidence     float64 `json:"confidence"`
	Reason         string  `json:"reason"`
	Quote          string  `json:"quote"`
}

const steamMaxContentLen = 6000

func ClassifySteamPost(ctx context.Context, c ai.Client, title, contents string) (*SteamClassification, error) {
	if c == nil || !c.IsEnabled() {
		return nil, ai.ErrAINotEnabled
	}
	body := contents
	if len(body) > steamMaxContentLen {
		body = body[:steamMaxContentLen]
	}
	user := fmt.Sprintf("title:\n%s\n\ncontents:\n%s", title, body)
	resp, err := c.Complete(ctx, ai.CompleteRequest{
		Model:     c.ModelFast(),
		System:    ai.SystemSteamClassify,
		User:      user,
		JSONMode:  true,
		MaxTokens: 300,
		CacheKey:  ai.HashKey("steam_classify", c.ModelFast(), title, body),
	})
	if err != nil {
		return nil, err
	}
	var cls SteamClassification
	if err := json.Unmarshal([]byte(resp.Content), &cls); err != nil {
		return nil, fmt.Errorf("steam classify parse: %w (raw: %s)", err, resp.Content)
	}
	return &cls, nil
}
