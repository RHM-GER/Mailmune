// Package events implements a small in-process fan-out hub used to stream
// agent status (scan progress, new decisions, connection state) to local
// subscribers such as the SSE endpoint.
package events

import (
	"encoding/json"
	"sync"
)

// Event is a single published message.
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Hub fans out events to all current subscribers. Publishing never blocks;
// slow subscribers drop events instead of stalling the agent.
type Hub struct {
	mu   sync.Mutex
	subs map[*Subscription]struct{}
}

// Subscription is a single subscriber's buffered event channel.
type Subscription struct {
	hub    *Hub
	ch     chan Event
	closed bool
}

const subscriptionBuffer = 256

// NewHub creates an empty hub.
func NewHub() *Hub {
	return &Hub{subs: map[*Subscription]struct{}{}}
}

// Publish sends an event to every subscriber. data is marshalled once and
// shared. Marshalling errors are dropped silently; event payloads are
// agent-controlled and must remain serializable.
func (h *Hub) Publish(typ string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	event := Event{Type: typ, Data: raw}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		select {
		case sub.ch <- event:
		default:
			// Slow consumer: drop the event rather than block the agent.
		}
	}
}

// Subscribe registers a new subscriber. Close must be called when done.
func (h *Hub) Subscribe() *Subscription {
	sub := &Subscription{hub: h, ch: make(chan Event, subscriptionBuffer)}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

// Events returns the subscriber's event channel. It is closed by Close.
func (s *Subscription) Events() <-chan Event { return s.ch }

// Close unregisters the subscriber and closes its channel. It is safe to
// call more than once.
func (s *Subscription) Close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	delete(s.hub.subs, s)
	close(s.ch)
}

// Subscribers reports the current number of subscribers (for tests/status).
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
