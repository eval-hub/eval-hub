package api

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateGitCloneURL checks that raw is an HTTP(S) git URL whose host is safe to
// clone from a job pod. It rejects private, loopback, link-local, and cluster-local
// destinations so they cannot be passed through as TEST_DATA_GIT_URL.
//
// Hostname checks use literal IPs and well-known suffixes only (no DNS lookup) so
// API validation does not depend on network reachability. The init container also
// re-validates and resolves the host before cloning.
func ValidateGitCloneURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("git url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid git url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("git url must use http or https scheme")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return fmt.Errorf("git url host is required")
	}
	if isBlockedGitHostname(host) {
		return fmt.Errorf("git url host %q is not allowed", host)
	}
	if ip := net.ParseIP(host); ip != nil && isBlockedGitIP(ip) {
		return fmt.Errorf("git url host address %q is not allowed", host)
	}
	return nil
}

// ValidateGitCloneURLAuth rejects plain HTTP when credentials will be used for the clone,
// so secret_ref / basic-auth material is not sent in the clear.
func ValidateGitCloneURLAuth(raw string, withCredentials bool) error {
	if !withCredentials {
		return nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid git url: %w", err)
	}
	if strings.EqualFold(u.Scheme, "http") {
		return fmt.Errorf("git url with credentials must use https scheme")
	}
	return nil
}

// ValidateGitCloneURLResolved runs ValidateGitCloneURL then resolves the hostname
// and rejects any address that is private, loopback, link-local, or unspecified.
// lookup may be nil to use net.LookupIP.
func ValidateGitCloneURLResolved(raw string, lookup func(host string) ([]net.IP, error)) error {
	if err := ValidateGitCloneURL(raw); err != nil {
		return err
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid git url: %w", err)
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil {
		return nil // literal IP already checked
	}
	if lookup == nil {
		lookup = net.LookupIP
	}
	ips, err := lookup(host)
	if err != nil {
		return fmt.Errorf("resolve git url host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("resolve git url host %q: no addresses", host)
	}
	for _, ip := range ips {
		if isBlockedGitIP(ip) {
			return fmt.Errorf("git url host %q resolves to disallowed address %s", host, ip)
		}
	}
	return nil
}

func isBlockedGitHostname(host string) bool {
	switch host {
	case "localhost", "localhost.localdomain", "metadata", "metadata.google.internal":
		return true
	}
	// Cluster-local and link-local DNS names (Kubernetes Services, mDNS, etc.).
	if strings.HasSuffix(host, ".cluster.local") ||
		strings.HasSuffix(host, ".svc") ||
		strings.HasSuffix(host, ".local") {
		return true
	}
	return false
}

func isBlockedGitIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}

// LooksLikeHexSHA reports whether s could be a git commit SHA — 7–40 hex chars
// (case-insensitive). Used as a syntactic check for refs and for resolved_sha values.
func LooksLikeHexSHA(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// ValidateResolvedSHA checks that sha is empty (callers treat empty as a no-op) or a
// plausible content identity (see LooksLikeHexSHA). Rejects whitespace and arbitrary
// strings so garbage cannot be persisted as resolved_sha.
func ValidateResolvedSHA(sha string) error {
	if sha == "" {
		return nil
	}
	if !LooksLikeHexSHA(sha) {
		return fmt.Errorf("resolved_sha %q must be 7-40 hex characters", sha)
	}
	return nil
}
