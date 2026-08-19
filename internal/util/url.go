package util

import (
	"net/url"
	"strings"
)

// DefaultGtAuthority is the canonical remote identity of the Gas Town
// operator's authoritative gastown source (see internal/activation.
// DefaultAuthority, which re-exports this). Lives here, not in
// internal/activation, so packages that must not import activation (e.g.
// internal/version, to avoid an activation -> daemon -> version ->
// activation cycle) can still verify a remote against the same identity.
const DefaultGtAuthority = "github.com/jamelt/gastown"

// CanonicalRemote normalizes a git remote URL (ssh, https, or scp-style) to a
// bare host/path form (e.g. "github.com/jamelt/gastown") so it can be
// compared for identity regardless of protocol or trailing ".git". Lives
// here rather than in internal/activation so packages that must not import
// activation (e.g. internal/version, to avoid an activation -> daemon ->
// version -> activation import cycle) can still verify a remote against the
// same authoritative identity activation itself enforces.
func CanonicalRemote(remote string) string {
	remote = strings.TrimSpace(strings.TrimSuffix(remote, ".git"))
	if strings.HasPrefix(remote, "git@") {
		remote = strings.TrimPrefix(remote, "git@")
		remote = strings.Replace(remote, ":", "/", 1)
		return remote
	}
	for _, prefix := range []string{"https://", "http://", "ssh://git@", "ssh://"} {
		remote = strings.TrimPrefix(remote, prefix)
	}
	if at := strings.Index(remote, "@"); at >= 0 {
		remote = remote[at+1:]
	}
	return remote
}

// RedactURL strips credentials from a URL for safe logging.
// "https://x-access-token:ghp_abc@github.com/org/repo" → "https://github.com/org/repo"
// SSH-style URLs (git@github.com:org/repo.git) are returned as-is.
// URLs that net/url can't parse but contain no credentials are returned as-is for debugging.
func RedactURL(rawURL string) string {
	// SSH-style URLs and non-standard transports don't use standard URL conventions.
	if !strings.Contains(rawURL, "://") {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		// Can't parse — return as-is if no credentials, otherwise mask it.
		if !strings.Contains(rawURL, "@") {
			return rawURL
		}
		return "<invalid URL>"
	}
	u.User = nil
	return u.String()
}
