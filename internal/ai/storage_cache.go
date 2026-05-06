package ai

import "time"

type storageCache interface {
	AICacheGet(key string) ([]byte, bool)
	AICacheSet(key string, value []byte, ttl time.Duration) error
}

type StorageCache struct {
	S storageCache
}

func (c StorageCache) Get(key string) ([]byte, bool) {
	if c.S == nil {
		return nil, false
	}
	return c.S.AICacheGet(key)
}

func (c StorageCache) Set(key string, value []byte, ttl time.Duration) error {
	if c.S == nil {
		return nil
	}
	return c.S.AICacheSet(key, value, ttl)
}
