// Package http exposes the shared gateway as a versioned REST API.
package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/asmseo/mdcix/internal/gateway"
)

// Server is a localhost-oriented REST API.
type Server struct {
	eng *gateway.Engine
	srv *http.Server
}

// New creates an API server on addr (default bind as given; prefer localhost).
func New(eng *gateway.Engine, addr string) *Server {
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	s := &Server{eng: eng}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/v1/outline", s.outline)
	mux.HandleFunc("/v1/read", s.read)
	mux.HandleFunc("/v1/sections", s.sections)
	mux.HandleFunc("/v1/read-budget", s.readBudget)
	s.srv = &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	return s
}

func (s *Server) ListenAndServe() error { return s.srv.ListenAndServe() }
func (s *Server) Close() error          { return s.srv.Close() }

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type urlBody struct {
	URL             string   `json:"url"`
	SectionIDs      []string `json:"section_ids"`
	DocumentVersion string   `json:"document_version"`
	Query           string   `json:"query"`
	Budget          int      `json:"budget"`
	Format          string   `json:"format"` // json|markdown
}

func (s *Server) outline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var b urlBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.URL == "" {
		http.Error(w, "url required", http.StatusBadRequest)
		return
	}
	out, err := s.eng.Outline(r.Context(), b.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) read(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var b urlBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.URL == "" {
		http.Error(w, "url required", http.StatusBadRequest)
		return
	}
	doc, err := s.eng.ReadPage(r.Context(), b.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if b.Format == "markdown" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(doc.Markdown))
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) sections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var b urlBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.URL == "" || len(b.SectionIDs) == 0 {
		http.Error(w, "url and section_ids required", http.StatusBadRequest)
		return
	}
	res, err := s.eng.ReadSectionsVersioned(r.Context(), b.URL, b.SectionIDs, b.Budget, b.DocumentVersion)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if b.Format == "markdown" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(res.Markdown))
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) readBudget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var b urlBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.URL == "" {
		http.Error(w, "url required", http.StatusBadRequest)
		return
	}
	if b.Budget <= 0 {
		b.Budget = 1500
	}
	res, err := s.eng.ReadBudget(r.Context(), b.URL, b.Query, b.Budget)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if b.Format == "markdown" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(res.Markdown))
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
