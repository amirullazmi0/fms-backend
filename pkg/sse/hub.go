// Package sse is a fan-out hub for Server-Sent Events: every subscriber gets every
// broadcast. Slow subscribers drop messages instead of blocking the publisher
// (the dashboard refetches on reconnect anyway).
package sse

import (
	"encoding/json"
	"sync"
)

type Hub struct {
	mu          sync.RWMutex
	subscribers map[chan []byte]struct{}
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[chan []byte]struct{})}
}

// Subscribe returns a channel of encoded JSON messages.
func (h *Hub) Subscribe() chan []byte {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subscribers[ch]; ok {
		delete(h.subscribers, ch)
		close(ch)
	}
}

// Count is the number of connected subscribers.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

// Broadcast encodes msg as JSON and sends it to every subscriber.
func (h *Hub) Broadcast(msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers {
		select {
		case ch <- data:
		default:
		}
	}
}
