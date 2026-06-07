// Package netguard provides a single, hardened SSRF guard shared by every
// subsystem that fetches a caller- or model-supplied URL (skill marketplace,
// workflow webhook steps, federation peers, MCP HTTP transport, the proxy, and
// the web API). Keeping one implementation here — depending only on the
// standard library so any package may import it without a cycle — means the
// loopback / link-local (cloud metadata) / private / ULA / multicast blocklist
// is defined once and cannot drift between callers.
package netguard

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

// AllowPrivate reports whether the private/loopback IP checks are disabled via
// CYNTR_SSRF_ALLOW_PRIVATE=1. Secure by default (off); an operator may opt in
// for a trusted internal deployment, and tests use it to reach local servers.
func AllowPrivate() bool { return os.Getenv("CYNTR_SSRF_ALLOW_PRIVATE") == "1" }

// Error is returned when a URL is rejected by the guard.
type Error struct{ Msg string }

func (e Error) Error() string { return "ssrf guard: " + e.Msg }

// ValidatePublicURL parses rawURL and rejects it unless it is an http(s) URL
// whose host resolves only to public (routable) IP addresses. It blocks
// loopback, link-local (incl. the cloud metadata endpoint 169.254.169.254),
// private, ULA, multicast, and unspecified ranges.
func ValidatePublicURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Error{"invalid URL"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Error{"only http and https are allowed"}
	}
	host := u.Hostname()
	if host == "" {
		return Error{"missing host"}
	}
	if AllowPrivate() {
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return Error{"cannot resolve host"}
	}
	for _, ip := range ips {
		if !IsPublicIP(ip) {
			return Error{fmt.Sprintf("host resolves to non-public address %s", ip)}
		}
	}
	return nil
}

// IsPublicIP reports whether ip is globally routable (not loopback, private,
// link-local, ULA, multicast, or unspecified).
func IsPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsPrivate() {
		return false
	}
	return true
}

// GuardedHTTPClient returns an http.Client with the given timeout whose
// redirects are re-validated through the guard — a public URL must not be
// allowed to 30x-redirect to an internal address.
func GuardedHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return ValidatePublicURL(req.URL.String())
		},
	}
}
