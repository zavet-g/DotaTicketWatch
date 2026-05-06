package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, value []byte, ttl time.Duration) error
}

func HashKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}
