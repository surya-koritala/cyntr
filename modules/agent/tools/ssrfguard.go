package tools

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

// ssrfAllowPrivate, when CYNTR_SSRF_ALLOW_PRIVATE=1, disables the private/
// loopback IP checks. Secure by default (off); an operator may opt in for a
// trusted internal deployment, and tests use it to reach local httptest servers.
func ssrfAllowPrivate() bool { return os.Getenv("CYNTR_SSRF_ALLOW_PRIVATE") == "1" }

// ssrfError is returned when a URL is rejected by the SSRF guard.
type ssrfError struct{ msg string }

func (e ssrfError) Error() string { return "ssrf guard: " + e.msg }

// ValidatePublicURL parses rawURL and rejects it unless it is an http(s) URL
// whose host resolves only to public (routable) IP addresses. It blocks
// loopback, link-local (incl. the cloud metadata endpoint 169.254.169.254),
// private, ULA, multicast, and unspecified ranges. Used by every server-side
// fetch tool so a model-supplied URL cannot reach internal services.
func ValidatePublicURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ssrfError{"invalid URL"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ssrfError{"only http and https are allowed"}
	}
	host := u.Hostname()
	if host == "" {
		return ssrfError{"missing host"}
	}
	if ssrfAllowPrivate() {
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return ssrfError{"cannot resolve host"}
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return ssrfError{fmt.Sprintf("host resolves to non-public address %s", ip)}
		}
	}
	return nil
}

// isPublicIP reports whether ip is globally routable (not loopback, private,
// link-local, ULA, multicast, or unspecified).
func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsPrivate() {
		return false
	}
	return true
}

// guardedHTTPClient returns an http.Client whose timeout is set and whose
// redirects are re-validated through the SSRF guard — a public URL must not be
// allowed to 30x-redirect to an internal address.
func guardedHTTPClient(timeout time.Duration) *http.Client {
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
