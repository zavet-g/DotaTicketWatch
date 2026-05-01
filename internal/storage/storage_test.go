package storage

import (
	"os"
	"testing"
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
}
