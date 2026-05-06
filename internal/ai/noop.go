package ai

import (
	"context"
	"time"
)

type noopClient struct{}

func (n *noopClient) IsEnabled() bool   { return false }
func (n *noopClient) ModelFast() string { return "" }
func (n *noopClient) ModelSmart() string { return "" }

func (n *noopClient) Complete(_ context.Context, _ CompleteRequest) (*CompleteResponse, error) {
	return nil, ErrAINotEnabled
}

type noopCache struct{}

func (noopCache) Get(_ string) ([]byte, bool)              { return nil, false }
func (noopCache) Set(_ string, _ []byte, _ time.Duration) error { return nil }
