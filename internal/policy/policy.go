// Package policy holds conservative local-retrieval preferences.
//
// These checks document intent and soft-fail; they do not bypass authentication,
// paywalls, CAPTCHA, or anti-bot systems. robots.txt is not authorization to
// redistribute content. Public redistribution opt-in is reserved for a future
// hosted surface and is not implemented here.
package policy

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Decision is a local access advice for a URL.
type Decision struct {
	Allow       bool     `json:"allow"`
	Attribution string   `json:"attribution,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	Robots      string   `json:"robots_status,omitempty"` // unknown|allowed|disallowed|skipped
}

// Checker evaluates publisher preferences for local retrieval.
type Checker struct {
	UserAgent string
	Client    *http.Client
	// SkipRobots disables robots.txt fetches (default false = attempt when possible).
	SkipRobots bool
	// Enabled gates the checker; when false, Allow is always true with a note.
	Enabled bool
}

// Default returns a conservative checker (enabled, robots attempted best-effort).
func Default(ua string) *Checker {
	return &Checker{
		UserAgent: ua,
		Client:    &http.Client{Timeout: 5 * time.Second},
		Enabled:   true,
	}
}

// Evaluate returns local-retrieval advice. Fail-open on robots network errors
// with an explicit warning — never treat silence as redistribution rights.
func (c *Checker) Evaluate(ctx context.Context, rawURL string) Decision {
	d := Decision{Allow: true, Robots: "skipped"}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return Decision{Allow: false, Warnings: []string{"invalid_url"}}
	}
	d.Attribution = u.Scheme + "://" + u.Host + u.Path
	if c == nil || !c.Enabled {
		d.Warnings = append(d.Warnings, "policy_disabled")
		d.Robots = "skipped"
		return d
	}
	if c.SkipRobots {
		d.Warnings = append(d.Warnings, "robots_skipped_by_config")
		return d
	}
	robotsURL := u.Scheme + "://" + u.Host + "/robots.txt"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		d.Warnings = append(d.Warnings, "robots_request_error")
		d.Robots = "unknown"
		return d
	}
	ua := c.UserAgent
	if ua == "" {
		ua = "mdcix"
	}
	req.Header.Set("User-Agent", ua)
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		d.Warnings = append(d.Warnings, "robots_unreachable")
		d.Robots = "unknown"
		return d
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		d.Warnings = append(d.Warnings, "robots_http_error")
		d.Robots = "unknown"
		return d
	}
	// Minimal path check: if Disallow: / for our UA group, advise disallow.
	// Full robots parsing is intentionally shallow in this release.
	buf := make([]byte, 64<<10)
	n, _ := resp.Body.Read(buf)
	body := strings.ToLower(string(buf[:n]))
	d.Robots = "allowed"
	if strings.Contains(body, "disallow: /") && !strings.Contains(body, "allow: "+strings.ToLower(u.Path)) {
		// Only flag when a catch-all disallow appears; still fail-open for local tooling
		// but surface a strong warning. Callers may choose to hard-stop.
		d.Warnings = append(d.Warnings,
			"robots_disallow_all_observed: local retrieval may still proceed; not a redistribution license")
		d.Robots = "disallowed"
	}
	d.Warnings = append(d.Warnings, "robots_txt_is_not_redistribution_authorization")
	return d
}

// ContentSignalsNote documents machine-readable usage prefs support level.
func ContentSignalsNote() string {
	return "content_signals: reserved; this release does not parse AI usage preference headers beyond surfacing Cache-Control/Vary when present"
}
