package security

import (
	"fmt"
	"strings"
)

// FailureClass categorizes non-successful retrieval/extraction.
type FailureClass string

const (
	FailNone       FailureClass = ""
	FailBlocked    FailureClass = "blocked"
	FailAuth       FailureClass = "auth_required"
	FailPaywall    FailureClass = "paywall"
	FailBotChallenge FailureClass = "bot_challenge"
	FailEmpty      FailureClass = "empty_extraction"
	FailHTTP       FailureClass = "http_error"
)

// ClassifyExtraction inspects status + bodies to detect failed extractions.
// Empty markdown must never be treated as successful savings.
func ClassifyExtraction(status int, contentType, originalBody, markdown string) error {
	if status >= 400 {
		return fmt.Errorf("%s: http %d", FailHTTP, status)
	}
	md := strings.TrimSpace(markdown)
	orig := strings.ToLower(originalBody)
	if looksLikeBotChallenge(orig) || looksLikeBotChallenge(strings.ToLower(md)) {
		return fmt.Errorf("%s: challenge or interstitial page", FailBotChallenge)
	}
	if looksLikeAuth(orig) {
		return fmt.Errorf("%s: authentication wall", FailAuth)
	}
	if looksLikePaywall(orig) {
		return fmt.Errorf("%s: paywall", FailPaywall)
	}
	if md == "" {
		return fmt.Errorf("%s: no extractable content", FailEmpty)
	}
	// Very short MD from large HTML → likely failed extract
	if len(originalBody) > 1500 && len(md) < 40 {
		return fmt.Errorf("%s: extraction too thin for body size", FailEmpty)
	}
	return nil
}

func looksLikeBotChallenge(s string) bool {
	// Strong interstitial markers — enough alone.
	strong := []string{
		"cf-browser-verification",
		"challenge-platform",
		"checking your browser before accessing",
		"verify you are human",
		"enable javascript and cookies to continue",
		"just a moment...",
	}
	for _, n := range strong {
		if strings.Contains(s, n) {
			return true
		}
	}
	// Weaker phrases appear in legitimate docs; require a combination.
	weak := 0
	for _, n := range []string{
		"captcha", "attention required", "access denied", "request blocked",
		"enable javascript", "checking your browser", "challenge-form",
	} {
		if strings.Contains(s, n) {
			weak++
		}
	}
	return weak >= 2
}

func looksLikeAuth(s string) bool {
	return strings.Contains(s, "sign in to continue") ||
		strings.Contains(s, "login required") ||
		strings.Contains(s, "www-authenticate")
}

func looksLikePaywall(s string) bool {
	// Prefer explicit paywall/subscription interstitials over mere mentions.
	if strings.Contains(s, "subscribe to read") || strings.Contains(s, "subscribers only") {
		return true
	}
	if strings.Contains(s, "paywall") &&
		(strings.Contains(s, "subscribe") || strings.Contains(s, "already a subscriber") ||
			strings.Contains(s, "to continue reading")) {
		return true
	}
	return false
}
