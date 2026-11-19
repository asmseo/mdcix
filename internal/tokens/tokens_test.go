package tokens

import "testing"

func TestApprox(t *testing.T) {
	if Approx("") != 0 {
		t.Fatal("empty should be 0")
	}
	if Approx("abcd") != 1 {
		t.Fatalf("got %d", Approx("abcd"))
	}
	if Approx("abcdefgh") != 2 {
		t.Fatalf("got %d", Approx("abcdefgh"))
	}
}

func TestCounterCompute(t *testing.T) {
	c, err := New("cl100k_base")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s := c.Compute("<html><body>"+repeat("word ", 200)+"</body></html>", "# Title\n\nShort body.\n", "converted")
	if s.Original <= s.Markdown {
		t.Fatalf("expected original > markdown: %+v", s)
	}
	if s.Saved <= 0 || s.Percent <= 0 {
		t.Fatalf("expected positive savings: %+v", s)
	}
	if s.Source != "converted" {
		t.Fatalf("source: %s", s.Source)
	}
}

func TestDefault(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if c.Count("hello world") <= 0 {
		t.Fatal("expected tokens")
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
