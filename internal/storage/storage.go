package storage

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketSubscribers  = []byte("subscribers")
	bucketNotified     = []byte("notified")
	bucketAICache      = []byte("ai_cache")
	bucketAIClassified = []byte("ai_classified")
	bucketAIState      = []byte("ai_state")
)

type Storage struct {
	db *bolt.DB
}

func New(path string) (*Storage, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range [][]byte{bucketSubscribers, bucketNotified, bucketAICache, bucketAIClassified, bucketAIState} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("init buckets: %w", err)
	}
	return &Storage{db: db}, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

type Subscriber struct {
	ChatID    int64  `json:"chat_id"`
	Username  string `json:"username"`
}

func (s *Storage) AddSubscriber(chatID int64, username string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSubscribers)
		data, _ := json.Marshal(Subscriber{ChatID: chatID, Username: username})
		return b.Put(int64ToKey(chatID), data)
	})
}

func (s *Storage) RemoveSubscriber(chatID int64) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketSubscribers).Delete(int64ToKey(chatID))
	})
}

func (s *Storage) IsSubscribed(chatID int64) bool {
	var found bool
	_ = s.db.View(func(tx *bolt.Tx) error {
		found = tx.Bucket(bucketSubscribers).Get(int64ToKey(chatID)) != nil
		return nil
	})
	return found
}

func (s *Storage) AllSubscribers() ([]Subscriber, error) {
	var subs []Subscriber
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketSubscribers).ForEach(func(_, v []byte) error {
			var sub Subscriber
			if err := json.Unmarshal(v, &sub); err == nil {
				subs = append(subs, sub)
			}
			return nil
		})
	})
	return subs, err
}

func (s *Storage) SubscriberCount() int {
	subs, _ := s.AllSubscribers()
	return len(subs)
}

func (s *Storage) AlreadyNotified(eventID string) bool {
	var found bool
	_ = s.db.View(func(tx *bolt.Tx) error {
		found = tx.Bucket(bucketNotified).Get([]byte(eventID)) != nil
		return nil
	})
	return found
}

func (s *Storage) NotifiedCount() int {
	var count int
	_ = s.db.View(func(tx *bolt.Tx) error {
		count = tx.Bucket(bucketNotified).Stats().KeyN
		return nil
	})
	return count
}

func (s *Storage) MarkNotified(eventID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketNotified).Put([]byte(eventID), []byte("1"))
	})
}

func int64ToKey(id int64) []byte {
	return []byte(fmt.Sprintf("%d", id))
}

func (s *Storage) AlreadyClassified(key string) bool {
	var found bool
	_ = s.db.View(func(tx *bolt.Tx) error {
		found = tx.Bucket(bucketAIClassified).Get([]byte(key)) != nil
		return nil
	})
	return found
}

func (s *Storage) MarkClassified(key string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		ts := make([]byte, 8)
		binary.BigEndian.PutUint64(ts, uint64(time.Now().Unix()))
		return tx.Bucket(bucketAIClassified).Put([]byte(key), ts)
	})
}

type aiCacheEntry struct {
	Value     []byte `json:"v"`
	ExpiresAt int64  `json:"e"`
}

func (s *Storage) AICacheGet(key string) ([]byte, bool) {
	var value []byte
	var ok bool
	_ = s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketAICache).Get([]byte(key))
		if raw == nil {
			return nil
		}
		var entry aiCacheEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil
		}
		if entry.ExpiresAt > 0 && time.Now().Unix() > entry.ExpiresAt {
			return nil
		}
		value = entry.Value
		ok = true
		return nil
	})
	return value, ok
}

func (s *Storage) AICacheSet(key string, value []byte, ttl time.Duration) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		entry := aiCacheEntry{Value: value}
		if ttl != 0 {
			entry.ExpiresAt = time.Now().Add(ttl).Unix()
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketAICache).Put([]byte(key), data)
	})
}

func (s *Storage) AIStateGet(key string) ([]byte, bool) {
	var value []byte
	var ok bool
	_ = s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketAIState).Get([]byte(key))
		if raw == nil {
			return nil
		}
		value = make([]byte, len(raw))
		copy(value, raw)
		ok = true
		return nil
	})
	return value, ok
}

func (s *Storage) AIStateSet(key string, value []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketAIState).Put([]byte(key), value)
	})
}
