package render

import (
	"testing"
)

func TestParseMode(t *testing.T) {
	if ParseMode("auto") != ModeAuto {
		t.Fatal("auto")
	}
	if ParseMode("on") != ModeOn {
		t.Fatal("on")
	}
	if ParseMode("") != ModeOff {
		t.Fatal("off")
	}
}

func TestNeedsAuto(t *testing.T) {
	if !NeedsAuto(stringsRepeat("x", 10000), "") {
		t.Fatal("empty md")
	}
	if !NeedsAuto(stringsRepeat("x", 10000), "short") {
		t.Fatal("thin md")
	}
	if NeedsAuto("<html>hi</html>", stringsRepeat("word ", 200)) {
		t.Fatal("rich md should not need auto")
	}
}

func stringsRepeat(s string, n int) string {
	b := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		b = append(b, s...)
	}
	return string(b)
}
