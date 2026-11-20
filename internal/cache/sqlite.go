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

const schemaVersion = "2"

// Store is a SQLite-backed document cache.
type Store struct {
	db         *sql.DB
	TTL        time.Duration
	MaxEntries int // 0 = unlimited
}

// EntryMeta is HTTP validation metadata stored alongside the document.
type EntryMeta struct {
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	CacheControl string `json:"cache_control,omitempty"`
	Vary         string `json:"vary,omitempty"`
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

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS documents (
  cache_key TEXT PRIMARY KEY,
  url TEXT NOT NULL,
  version TEXT NOT NULL,
  retrieved_at INTEGER NOT NULL,
  payload BLOB NOT NULL,
  etag TEXT,
  last_modified TEXT,
  cache_control TEXT,
  vary TEXT,
  bytes INTEGER DEFAULT 0
);
CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_documents_retrieved ON documents(retrieved_at);
`)
	if err != nil {
		return err
	}
	// Additive columns for older DBs created before schema v2.
	for _, ddl := range []string{
		`ALTER TABLE documents ADD COLUMN etag TEXT`,
		`ALTER TABLE documents ADD COLUMN last_modified TEXT`,
		`ALTER TABLE documents ADD COLUMN cache_control TEXT`,
		`ALTER TABLE documents ADD COLUMN vary TEXT`,
		`ALTER TABLE documents ADD COLUMN bytes INTEGER DEFAULT 0`,
	} {
		_, _ = s.db.Exec(ddl) // ignore "duplicate column" on fresh DBs
	}
	_, err = s.db.Exec(`INSERT INTO meta(k,v) VALUES('schema_version', ?)
		ON CONFLICT(k) DO UPDATE SET v=excluded.v`, schemaVersion)
	return err
}

// Close closes the DB.
func (s *Store) Close() error { return s.db.Close() }

// Key builds a cache key (URL + representation params). Do not use canonical alone.
func Key(url, mode, encoding string) string {
	return KeyFull(url, mode, encoding, false, "off")
}

// KeyFull includes plain-text and JS-render representation knobs.
func KeyFull(url, mode, encoding string, plain bool, renderJS string) string {
	if renderJS == "" {
		renderJS = "off"
	}
	return fmt.Sprintf("%s|mode=%s|enc=%s|plain=%t|js=%s", url, mode, encoding, plain, renderJS)
}

// Get returns a cached document if fresh.
func (s *Store) Get(key string) (document.Document, bool, error) {
	doc, _, ok, err := s.GetWithMeta(key)
	return doc, ok, err
}

// GetWithMeta returns document + HTTP validators when fresh.
func (s *Store) GetWithMeta(key string) (document.Document, EntryMeta, bool, error) {
	var payload []byte
	var retrieved int64
	var meta EntryMeta
	err := s.db.QueryRow(`
SELECT payload, retrieved_at, IFNULL(etag,''), IFNULL(last_modified,''), IFNULL(cache_control,''), IFNULL(vary,'')
FROM documents WHERE cache_key = ?`, key).
		Scan(&payload, &retrieved, &meta.ETag, &meta.LastModified, &meta.CacheControl, &meta.Vary)
	if err == sql.ErrNoRows {
		return document.Document{}, EntryMeta{}, false, nil
	}
	if err != nil {
		return document.Document{}, EntryMeta{}, false, err
	}
	if noStore(meta.CacheControl) {
		return document.Document{}, meta, false, nil
	}
	if time.Since(time.Unix(retrieved, 0)) > s.TTL {
		return document.Document{}, meta, false, nil
	}
	var doc document.Document
	if err := json.Unmarshal(payload, &doc); err != nil {
		return document.Document{}, EntryMeta{}, false, err
	}
	return doc, meta, true, nil
}

// Put stores a document.
func (s *Store) Put(key string, doc document.Document) error {
	return s.PutWithMeta(key, doc, EntryMeta{})
}

// PutWithMeta stores a document and HTTP validators.
func (s *Store) PutWithMeta(key string, doc document.Document, meta EntryMeta) error {
	if noStore(meta.CacheControl) {
		return nil
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO documents(cache_key, url, version, retrieved_at, payload, etag, last_modified, cache_control, vary, bytes)
		 VALUES(?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(cache_key) DO UPDATE SET
		   version=excluded.version, retrieved_at=excluded.retrieved_at, payload=excluded.payload,
		   etag=excluded.etag, last_modified=excluded.last_modified,
		   cache_control=excluded.cache_control, vary=excluded.vary, bytes=excluded.bytes`,
		key, doc.URL, doc.Version, time.Now().Unix(), b,
		meta.ETag, meta.LastModified, meta.CacheControl, meta.Vary, len(b),
	)
	if err != nil {
		return err
	}
	return s.evictIfNeeded()
}

// Invalidate removes one key.
func (s *Store) Invalidate(key string) error {
	_, err := s.db.Exec(`DELETE FROM documents WHERE cache_key = ?`, key)
	return err
}

// Clear deletes all entries.
func (s *Store) Clear() error {
	_, err := s.db.Exec(`DELETE FROM documents`)
	return err
}

// Stats counts entries (legacy helper).
func (s *Store) Stats() (n int, err error) {
	st, err := s.StatsDetail()
	return st.Entries, err
}

// Detail is aggregate cache statistics.
type Detail struct {
	Entries       int    `json:"entries"`
	ApproxBytes   int64  `json:"approx_bytes"`
	SchemaVersion string `json:"schema_version"`
	TTLSeconds    int64  `json:"ttl_seconds"`
	MaxEntries    int    `json:"max_entries"`
}

// StatsDetail returns richer stats.
func (s *Store) StatsDetail() (Detail, error) {
	var d Detail
	d.TTLSeconds = int64(s.TTL / time.Second)
	d.MaxEntries = s.MaxEntries
	err := s.db.QueryRow(`SELECT COUNT(*), IFNULL(SUM(bytes),0) FROM documents`).Scan(&d.Entries, &d.ApproxBytes)
	if err != nil {
		return d, err
	}
	_ = s.db.QueryRow(`SELECT v FROM meta WHERE k='schema_version'`).Scan(&d.SchemaVersion)
	if d.SchemaVersion == "" {
		d.SchemaVersion = schemaVersion
	}
	return d, nil
}

func (s *Store) evictIfNeeded() error {
	if s.MaxEntries <= 0 {
		return nil
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&n); err != nil {
		return err
	}
	if n <= s.MaxEntries {
		return nil
	}
	// Evict oldest retrieved_at first.
	_, err := s.db.Exec(`
DELETE FROM documents WHERE cache_key IN (
  SELECT cache_key FROM documents ORDER BY retrieved_at ASC LIMIT ?
)`, n-s.MaxEntries)
	return err
}

func noStore(cc string) bool {
	cc = strings.ToLower(cc)
	return strings.Contains(cc, "no-store") || strings.Contains(cc, "private")
}
