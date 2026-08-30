package events

import (
	"encoding/json"
	"testing"
)

func TestPublishReachesSubscribers(t *testing.T) {
	hub := NewHub()
	first := hub.Subscribe()
	second := hub.Subscribe()
	defer first.Close()
	defer second.Close()

	hub.Publish("scan.progress", map[string]int{"processed": 3})

	for _, sub := range []*Subscription{first, second} {
		select {
		case event := <-sub.Events():
			if event.Type != "scan.progress" {
				t.Fatalf("event type = %q", event.Type)
			}
			var payload map[string]int
			if err := json.Unmarshal(event.Data, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["processed"] != 3 {
				t.Fatalf("payload = %+v", payload)
			}
		default:
			t.Fatal("subscriber did not receive the event")
		}
	}
}

func TestSlowSubscriberDropsInsteadOfBlocking(t *testing.T) {
	hub := NewHub()
	sub := hub.Subscribe()
	defer sub.Close()

	done := make(chan struct{})
	go func() {
		for i := 0; i < subscriptionBuffer*4; i++ {
			hub.Publish("scan.progress", map[string]int{"i": i})
		}
		close(done)
	}()
	<-done // Publish must never block, even for a subscriber that never reads.

	received := 0
	for {
		select {
		case _, ok := <-sub.Events():
			if !ok {
				t.Fatal("channel closed unexpectedly")
			}
			received++
			continue
		default:
		}
		break
	}
	if received > subscriptionBuffer {
		t.Fatalf("received %d events, buffer is %d", received, subscriptionBuffer)
	}
}

func TestCloseUnregisters(t *testing.T) {
	hub := NewHub()
	sub := hub.Subscribe()
	sub.Close()
	sub.Close() // second close must not panic
	if hub.Subscribers() != 0 {
		t.Fatalf("subscribers = %d, want 0", hub.Subscribers())
	}
	hub.Publish("scan.finished", map[string]bool{"ok": true}) // must not panic on closed channel
}
