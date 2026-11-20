package cache

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/asmseo/mdcix/internal/document"
)

func TestDBPath(t *testing.T) {
	if DBPath("") != "" {
		t.Fatal("empty")
	}
	if got := DBPath("/data"); got != filepath.Join("/data", "mdcix.db") {
		t.Fatalf("got %s", got)
	}
	if got := DBPath("/data/custom.db"); got != "/data/custom.db" {
		t.Fatalf("got %s", got)
	}
}

func TestPutGetClear(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(DBPath(dir), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	doc := document.Document{URL: "https://ex.test", Version: "abc", Markdown: "# Hi"}
	key := Key(doc.URL, "reader", "cl100k_base")
	if err := store.Put(key, doc); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.Get(key)
	if err != nil || !ok {
		t.Fatalf("get ok=%v err=%v", ok, err)
	}
	if got.Version != "abc" {
		t.Fatalf("%+v", got)
	}
	n, err := store.Stats()
	if err != nil || n != 1 {
		t.Fatalf("stats n=%d err=%v", n, err)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	n, _ = store.Stats()
	if n != 0 {
		t.Fatalf("cleared n=%d", n)
	}
}
