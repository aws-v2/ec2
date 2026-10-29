package notifications

import (
	"encoding/json"
	"sync"
	"time"
)

type Notification struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Message   string `json:"message,omitempty"`
	Status    string `json:"status"` // running | completed | failed | info
	Progress  *int   `json:"progress,omitempty"`
	UpdatedAt string `json:"updatedAt"`
}

type Hub struct {
	mu     sync.RWMutex
	subs   map[string]map[chan []byte]struct{} // userID -> subscribers (one per open tab)
	active map[string]map[string]Notification  // userID -> jobID -> latest running state
}

func NewHub() *Hub {
	return &Hub{
		subs:   map[string]map[chan []byte]struct{}{},
		active: map[string]map[string]Notification{},
	}
}

// Subscribe returns a channel of encoded events plus the currently running jobs,
// so a page refresh gets the progress bars back immediately.
func (h *Hub) Subscribe(userID string) (chan []byte, []Notification, func()) {
	ch := make(chan []byte, 32)

	h.mu.Lock()
	if h.subs[userID] == nil {
		h.subs[userID] = map[chan []byte]struct{}{}
	}
	h.subs[userID][ch] = struct{}{}
	var snapshot []Notification
	for _, n := range h.active[userID] {
		snapshot = append(snapshot, n)
	}
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		delete(h.subs[userID], ch)
		h.mu.Unlock()
		close(ch)
	}
	return ch, snapshot, unsub
}

func (h *Hub) Publish(userID string, n Notification) {
	n.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	h.mu.Lock()
	if n.Status == "running" {
		if h.active[userID] == nil {
			h.active[userID] = map[string]Notification{}
		}
		h.active[userID][n.ID] = n
	} else if h.active[userID] != nil {
		delete(h.active[userID], n.ID)
	}
	subs := make([]chan []byte, 0, len(h.subs[userID]))
	for ch := range h.subs[userID] {
		subs = append(subs, ch)
	}
	h.mu.Unlock()

	b, _ := json.Marshal(n)
	for _, ch := range subs {
		select {
		case ch <- b:
		default: // slow client: drop this tick, the next one replaces it
		}
	}
}



