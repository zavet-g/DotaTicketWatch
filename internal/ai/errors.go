package ai

import "errors"

var (
	ErrAINotEnabled = errors.New("ai client not enabled")
	ErrTimeout      = errors.New("ai request timeout")
	ErrRateLimit         = errors.New("ai rate limit")
	ErrInsufficientQuota = errors.New("ai insufficient quota")
	ErrUnauthorized      = errors.New("ai unauthorized")
	ErrBadResponse  = errors.New("ai bad response")
)
