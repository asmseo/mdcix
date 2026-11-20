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

func TestPutGetClearMetaEvict(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(DBPath(dir), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.MaxEntries = 2

	doc := document.Document{URL: "https://ex.test/a", Version: "abc", Markdown: "# Hi"}
	key := Key(doc.URL, "reader", "cl100k_base")
	if err := store.PutWithMeta(key, doc, EntryMeta{ETag: `"v1"`, LastModified: "Wed, 01 Jan 2020 00:00:00 GMT"}); err != nil {
		t.Fatal(err)
	}
	got, meta, ok, err := store.GetWithMeta(key)
	if err != nil || !ok {
		t.Fatalf("get ok=%v err=%v", ok, err)
	}
	if got.Version != "abc" || meta.ETag != `"v1"` {
		t.Fatalf("%+v meta=%+v", got, meta)
	}

	// no-store must not persist
	if err := store.PutWithMeta(Key("https://ex.test/nostore", "reader", "cl100k_base"), doc, EntryMeta{CacheControl: "no-store"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.Get(Key("https://ex.test/nostore", "reader", "cl100k_base")); ok {
		t.Fatal("no-store should not cache")
	}

	_ = store.Put(Key("https://ex.test/b", "reader", "cl100k_base"), document.Document{URL: "b", Version: "b"})
	_ = store.Put(Key("https://ex.test/c", "reader", "cl100k_base"), document.Document{URL: "c", Version: "c"})
	n, _ := store.Stats()
	if n > 2 {
		t.Fatalf("evict failed n=%d", n)
	}

	st, err := store.StatsDetail()
	if err != nil || st.SchemaVersion == "" {
		t.Fatalf("%+v err=%v", st, err)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	n, _ = store.Stats()
	if n != 0 {
		t.Fatalf("cleared n=%d", n)
	}
}
