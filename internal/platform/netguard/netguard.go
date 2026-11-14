// Package netguard builds HTTP clients for outbound requests whose target URL
// is supplied by a merchant (notify_url, return_url, printer endpoints).
//
// The threat is server-side request forgery: a merchant registers an order with
// notify_url=http://127.0.0.1:8080/internal and the platform happily issues a
// GET from inside the trust boundary. Two layers stop it:
//
//   - the dialer refuses to connect to loopback, private, link-local, CGNAT and
//     other non-public addresses. The check runs after DNS resolution, so a
//     public hostname pointing at 127.0.0.1 (DNS rebinding) is caught too.
//   - redirects are re-validated and capped, so an attacker-controlled 302
//     cannot hop into the internal network.
//
// Operators who really do run merchant notification receivers on the same host
// or a LAN can opt into that with AllowPrivate. Link-local addresses, the cloud
// metadata range and multicast stay blocked either way: no legitimate merchant
// notification receiver lives at 169.254.169.254.
package netguard

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// DefaultTimeout bounds one notification round trip. Without it a merchant
// server that accepts the connection and never answers parks the caller
// forever, which is exactly how a single merchant can stall the outbox worker.
const DefaultTimeout = 15 * time.Second

// maxRedirects matches the net/http default; the difference is that each hop is
// validated before it is followed.
const maxRedirects = 5

// Options configures the outbound client.
type Options struct {
	// Timeout bounds one request including redirects and body read.
	Timeout time.Duration
	// AllowPrivate permits loopback and private (RFC 1918 / ULA) destinations
	// for self-hosted deployments where the merchant site runs on the same host
	// or LAN. Link-local, metadata and multicast stay blocked.
	AllowPrivate bool
}

// ErrBlockedTarget reports a URL or resolved address that the guard rejected.
var ErrBlockedTarget = errors.New("outbound request target is not allowed")

// Client returns an HTTP client that refuses non-public destinations.
func Client(options Options) *http.Client {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   control(options.AllowPrivate),
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			// No environment proxy: a proxy would hide the real destination
			// from the dial guard and turn an SSRF attempt into a proxy request.
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          32,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       60 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("%w: too many redirects", ErrBlockedTarget)
			}
			if err := ValidateURL(request.URL); err != nil {
				return err
			}
			return nil
		},
	}
}

// ValidateURL checks the shape of a merchant supplied URL against the strict
// policy. It is the cheap gate used at order creation; Client is the enforced
// gate at request time.
func ValidateURL(target *url.URL) error {
	return ValidateURLWith(target, false)
}

// ValidateURLWith checks the shape of a merchant supplied URL. allowPrivate
// relaxes the private range rule so an operator can keep LAN receivers working.
// A literal loopback, link-local or metadata address is always refused.
func ValidateURLWith(target *url.URL, allowPrivate bool) error {
	if target == nil {
		return fmt.Errorf("%w: empty url", ErrBlockedTarget)
	}
	scheme := strings.ToLower(target.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: scheme %q", ErrBlockedTarget, target.Scheme)
	}
	if target.User != nil {
		return fmt.Errorf("%w: embedded credentials", ErrBlockedTarget)
	}
	host := target.Hostname()
	if host == "" {
		return fmt.Errorf("%w: missing host", ErrBlockedTarget)
	}
	if ip := net.ParseIP(host); ip != nil {
		// Link-local and metadata addresses stay blocked even when loopback and
		// private ranges are allowed.
		if blocked(ip, allowPrivate) {
			return fmt.Errorf("%w: address %s", ErrBlockedTarget, ip)
		}
		return nil
	}
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		if !allowPrivate {
			return fmt.Errorf("%w: host %s", ErrBlockedTarget, host)
		}
	}
	return nil
}

// IsLocalTarget reports whether a URL points at this host or a private network.
// It is used to keep features that only make sense on a developer machine (the
// sandbox payment channel) from being registered on a publicly reachable
// deployment.
func IsLocalTarget(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified()
}

// ParseURL validates a raw string.
func ParseURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBlockedTarget, err)
	}
	return ValidateURL(parsed)
}

// control returns the dialer hook that inspects the resolved address.
func control(allowPrivate bool) func(network, address string, conn syscall.RawConn) error {
	return func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("%w: unparsable address %q", ErrBlockedTarget, address)
		}
		if blocked(ip, allowPrivate) {
			return fmt.Errorf("%w: address %s", ErrBlockedTarget, ip)
		}
		return nil
	}
}

// blocked classifies an address. allowPrivate relaxes the loopback and RFC 1918
// / ULA classes; the ranges that reach a cloud metadata service or a link-local
// neighbour stay blocked unconditionally.
func blocked(ip net.IP, allowPrivate bool) bool {
	if ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	// 0.0.0.0/8: "this network" plus the 0.0.0.0 wildcard that some stacks
	// treat as loopback.
	if v4 := ip.To4(); v4 != nil && v4[0] == 0 {
		return true
	}
	// 100.64.0.0/10 carrier grade NAT and 198.18.0.0/15 benchmarking are never a
	// merchant endpoint, switch or not.
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
		if v4[0] == 198 && (v4[1] == 18 || v4[1] == 19) {
			return true
		}
	}
	if allowPrivate {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}
