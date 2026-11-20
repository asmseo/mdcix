// Package cache provides local SQLite persistence for structured documents.
package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/asmseo/mdcix/internal/document"
)

// Store is a SQLite-backed document cache.
type Store struct {
	db  *sql.DB
	TTL time.Duration
}

// DBPath resolves a cache directory or explicit .db file path.
func DBPath(cacheDirOrFile string) string {
	if cacheDirOrFile == "" {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(cacheDirOrFile), ".db") {
		return cacheDirOrFile
	}
	return filepath.Join(cacheDirOrFile, "mdcix.db")
}

// Open opens or creates the database at path.
func Open(path string, ttl time.Duration) (*Store, error) {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, TTL: ttl}
	if s.TTL <= 0 {
		s.TTL = 24 * time.Hour
	}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS documents (
  cache_key TEXT PRIMARY KEY,
  url TEXT NOT NULL,
  version TEXT NOT NULL,
  retrieved_at INTEGER NOT NULL,
  payload BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL);
`)
	return err
}

// Close closes the DB.
func (s *Store) Close() error { return s.db.Close() }

// Key builds a cache key (URL + representation params). Do not use canonical alone.
func Key(url, mode, encoding string) string {
	return fmt.Sprintf("%s|mode=%s|enc=%s", url, mode, encoding)
}

// Get returns a cached document if fresh.
func (s *Store) Get(key string) (document.Document, bool, error) {
	var payload []byte
	var retrieved int64
	err := s.db.QueryRow(`SELECT payload, retrieved_at FROM documents WHERE cache_key = ?`, key).
		Scan(&payload, &retrieved)
	if err == sql.ErrNoRows {
		return document.Document{}, false, nil
	}
	if err != nil {
		return document.Document{}, false, err
	}
	if time.Since(time.Unix(retrieved, 0)) > s.TTL {
		return document.Document{}, false, nil
	}
	var doc document.Document
	if err := json.Unmarshal(payload, &doc); err != nil {
		return document.Document{}, false, err
	}
	return doc, true, nil
}

// Put stores a document.
func (s *Store) Put(key string, doc document.Document) error {
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO documents(cache_key, url, version, retrieved_at, payload) VALUES(?,?,?,?,?)
		 ON CONFLICT(cache_key) DO UPDATE SET version=excluded.version, retrieved_at=excluded.retrieved_at, payload=excluded.payload`,
		key, doc.URL, doc.Version, time.Now().Unix(), b,
	)
	return err
}

// Clear deletes all entries.
func (s *Store) Clear() error {
	_, err := s.db.Exec(`DELETE FROM documents`)
	return err
}

// Stats returns basic counts.
func (s *Store) Stats() (n int, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&n)
	return
}
