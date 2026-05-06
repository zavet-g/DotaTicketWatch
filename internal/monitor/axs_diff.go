package monitor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/artem/dotaticketwatch/internal/ai"
)

type AXSDiffSignal struct {
	IsPreSaleSignal bool     `json:"is_pre_sale_signal"`
	Confidence      float64  `json:"confidence"`
	Summary         string   `json:"summary"`
	Changes         []string `json:"changes"`
}

const axsDiffMaxLen = 30000

func AnalyzeAXSDiff(ctx context.Context, c ai.Client, oldJSON, newJSON string) (*AXSDiffSignal, error) {
	if c == nil || !c.IsEnabled() {
		return nil, ai.ErrAINotEnabled
	}
	if len(oldJSON) > axsDiffMaxLen {
		oldJSON = oldJSON[:axsDiffMaxLen]
	}
	if len(newJSON) > axsDiffMaxLen {
		newJSON = newJSON[:axsDiffMaxLen]
	}
	user := fmt.Sprintf("OLD:\n%s\n\nNEW:\n%s", oldJSON, newJSON)
	resp, err := c.Complete(ctx, ai.CompleteRequest{
		Model:     c.ModelFast(),
		System:    ai.SystemAXSDiff,
		User:      user,
		JSONMode:  true,
		MaxTokens: 500,
		CacheKey:  ai.HashKey("axs_diff", c.ModelFast(), oldJSON, newJSON),
	})
	if err != nil {
		return nil, err
	}
	var sig AXSDiffSignal
	if err := json.Unmarshal([]byte(resp.Content), &sig); err != nil {
		return nil, fmt.Errorf("axs diff parse: %w (raw: %s)", err, resp.Content)
	}
	return &sig, nil
}
