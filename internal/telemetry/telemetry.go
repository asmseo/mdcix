// Package telemetry is local, opt-in observability.
//
// Telemetry is DISABLED by default. When disabled, all Record calls are no-ops
// and no credentials, query text, or request bodies are stored.
package telemetry

import (
	"sync"
	"time"
)

// Event is a coarse operational signal (no secrets).
type Event struct {
	Name      string            `json:"name"`
	At        time.Time         `json:"at"`
	Duration  time.Duration     `json:"duration,omitempty"`
	Ints      map[string]int    `json:"ints,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// Sink receives events when enabled.
type Sink interface {
	Record(Event)
}

// Config toggles telemetry.
type Config struct {
	Enabled bool
}

// Bus is an in-memory sink (tests / local debug).
type Bus struct {
	mu     sync.Mutex
	events []Event
}

// Record appends an event.
func (b *Bus) Record(e Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	b.events = append(b.events, e)
}

// Snapshot returns a copy of recorded events.
func (b *Bus) Snapshot() []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Event, len(b.events))
	copy(out, b.events)
	return out
}

// Nop never records.
type Nop struct{}

// Record implements Sink.
func (Nop) Record(Event) {}

// FromConfig returns Nop unless Enabled.
func FromConfig(cfg Config) Sink {
	if !cfg.Enabled {
		return Nop{}
	}
	return &Bus{}
}
