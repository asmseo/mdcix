// Package stats aggregates token savings for a proxy session.
package stats

import (
	"sync"

	"github.com/asmseo/mdcix/internal/tokens"
)

// Session holds cumulative savings across requests.
type Session struct {
	mu        sync.Mutex
	Requests  int
	Original  int
	Markdown  int
	Saved     int
	Converted int
	Native    int
}

// Add records one request's savings.
func (s *Session) Add(sv tokens.Savings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Requests++
	s.Original += sv.Original
	s.Markdown += sv.Markdown
	s.Saved += sv.Saved
	if sv.Source == "native" {
		s.Native++
	} else {
		s.Converted++
	}
}

// Snapshot returns a copy of current totals.
func (s *Session) Snapshot() Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Session{
		Requests:  s.Requests,
		Original:  s.Original,
		Markdown:  s.Markdown,
		Saved:     s.Saved,
		Converted: s.Converted,
		Native:    s.Native,
	}
}

// Percent returns overall saved percent (0–100).
func (s *Session) Percent() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Original == 0 {
		return 0
	}
	return float64(s.Saved) / float64(s.Original) * 100
}
