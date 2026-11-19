package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yml")
	if err := os.WriteFile(path, []byte("listen: \":9090\"\nmode: agent\nmitm: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadYAML(path, Default())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9090" || cfg.Mode != "agent" || cfg.MITM {
		t.Fatalf("%+v", cfg)
	}
}

func TestDefault(t *testing.T) {
	d := Default()
	if d.Listen != ":8080" || d.Mode != "reader" {
		t.Fatalf("%+v", d)
	}
}
