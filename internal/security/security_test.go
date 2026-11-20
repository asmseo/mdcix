package security

import "testing"

func TestValidatePublicURL(t *testing.T) {
	if err := ValidatePublicURL("https://example.com/x"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePublicURL("file:///etc/passwd"); err == nil {
		t.Fatal("expected scheme error")
	}
	if err := ValidatePublicURL("http://127.0.0.1/"); err == nil {
		t.Fatal("expected localhost block")
	}
	if err := ValidatePublicURL("http://169.254.169.254/latest"); err == nil {
		t.Fatal("expected metadata block")
	}
}

func TestClassifyExtraction(t *testing.T) {
	if err := ClassifyExtraction(200, "text/html", "<html>ok</html>", "# Hi\n\nBody text here"); err != nil {
		t.Fatal(err)
	}
	if err := ClassifyExtraction(200, "text/html", "cf-browser-verification challenge", ""); err == nil {
		t.Fatal("expected bot challenge")
	}
	// Legitimate docs may mention CAPTCHA/paywalls without being an interstitial.
	if err := ClassifyExtraction(200, "text/html",
		"<html><body><p>We do not solve CAPTCHA or paywalls.</p></body></html>",
		"# Policy\n\nWe do not solve CAPTCHA or paywalls."); err != nil {
		t.Fatalf("false positive failure class: %v", err)
	}
	if err := ClassifyExtraction(200, "text/html", "<html>"+repeat("x", 2000)+"</html>", "  "); err == nil {
		t.Fatal("expected empty")
	}
	if err := ClassifyExtraction(403, "text/html", "nope", "x"); err == nil {
		t.Fatal("expected http error")
	}
}

func repeat(s string, n int) string {
	b := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		b = append(b, s...)
	}
	return string(b)
}
