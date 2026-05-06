package storage

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func TestStorage(t *testing.T) {
	path := t.TempDir() + "/test.db"
	s, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		s.Close()
		os.Remove(path)
	}()

	t.Run("subscribers", func(t *testing.T) {
		if s.IsSubscribed(1) {
			t.Error("user should not be subscribed initially")
		}

		if err := s.AddSubscriber(1, "alice"); err != nil {
			t.Fatalf("AddSubscriber: %v", err)
		}
		if !s.IsSubscribed(1) {
			t.Error("user should be subscribed after add")
		}

		subs, err := s.AllSubscribers()
		if err != nil {
			t.Fatalf("AllSubscribers: %v", err)
		}
		if len(subs) != 1 || subs[0].ChatID != 1 {
			t.Errorf("expected 1 subscriber with ID 1, got %v", subs)
		}

		if err := s.RemoveSubscriber(1); err != nil {
			t.Fatalf("RemoveSubscriber: %v", err)
		}
		if s.IsSubscribed(1) {
			t.Error("user should not be subscribed after remove")
		}
		if s.SubscriberCount() != 0 {
			t.Error("expected 0 subscribers after remove")
		}
	})

	t.Run("deduplication", func(t *testing.T) {
		if s.AlreadyNotified("event-123") {
			t.Error("event should not be notified initially")
		}
		if err := s.MarkNotified("event-123"); err != nil {
			t.Fatalf("MarkNotified: %v", err)
		}
		if !s.AlreadyNotified("event-123") {
			t.Error("event should be marked as notified")
		}
		if s.AlreadyNotified("event-456") {
			t.Error("different event should not be marked")
		}
	})

	t.Run("ai_classified", func(t *testing.T) {
		if s.AlreadyClassified("steam:abc") {
			t.Error("not classified initially")
		}
		if err := s.MarkClassified("steam:abc"); err != nil {
			t.Fatalf("MarkClassified: %v", err)
		}
		if !s.AlreadyClassified("steam:abc") {
			t.Error("should be classified after mark")
		}
		if s.AlreadyClassified("steam:xyz") {
			t.Error("different key should not be classified")
		}
	})

	t.Run("ai_cache", func(t *testing.T) {
		if _, ok := s.AICacheGet("k1"); ok {
			t.Error("empty cache should miss")
		}
		val := []byte(`{"a":1}`)
		if err := s.AICacheSet("k1", val, time.Hour); err != nil {
			t.Fatalf("AICacheSet: %v", err)
		}
		got, ok := s.AICacheGet("k1")
		if !ok {
			t.Fatal("should hit after set")
		}
		if !bytes.Equal(got, val) {
			t.Errorf("got %s, want %s", got, val)
		}

		if err := s.AICacheSet("k2", []byte("expired"), -time.Second); err != nil {
			t.Fatalf("AICacheSet: %v", err)
		}
		if _, ok := s.AICacheGet("k2"); ok {
			t.Error("expired entry should miss")
		}
	})

	t.Run("ai_state", func(t *testing.T) {
		if _, ok := s.AIStateGet("snap"); ok {
			t.Error("empty state should miss")
		}
		if err := s.AIStateSet("snap", []byte("hello")); err != nil {
			t.Fatalf("AIStateSet: %v", err)
		}
		got, ok := s.AIStateGet("snap")
		if !ok {
			t.Fatal("should hit after set")
		}
		if string(got) != "hello" {
			t.Errorf("got %s", got)
		}
		if err := s.AIStateSet("snap", []byte("world")); err != nil {
			t.Fatalf("AIStateSet: %v", err)
		}
		got, _ = s.AIStateGet("snap")
		if string(got) != "world" {
			t.Errorf("overwrite failed: %s", got)
		}
	})
}
