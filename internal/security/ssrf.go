// Package security hardens fetching and classifies retrieval failures.
package security

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidatePublicURL ensures scheme is http(s) and host is not private/metadata.
func ValidatePublicURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme not allowed: %s", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}
	if isBlockedHost(host) {
		return fmt.Errorf("host blocked: %s", host)
	}
	// If host is an IP literal, check it
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("private ip blocked: %s", host)
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS failure is OK to report; caller may still try — we fail closed for public fetcher
		return fmt.Errorf("dns lookup: %w", err)
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("resolved private ip blocked for %s: %s", host, ip)
		}
	}
	return nil
}

func isBlockedHost(host string) bool {
	h := strings.ToLower(host)
	switch h {
	case "localhost", "metadata.google.internal", "metadata":
		return true
	}
	if strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") {
		return true
	}
	return false
}

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// Cloud metadata 169.254.169.254
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}
	return false
}
