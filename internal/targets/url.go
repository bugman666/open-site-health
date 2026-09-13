package targets

import (
	"fmt"
	"net/url"
	"strings"
)

// normalizeURL trims input, requires an absolute http(s) URL with a host,
// lower-cases the scheme/host, drops fragments and default ports, and
// strips a trailing slash on non-root paths. The result is used both as
// the stored value and as the duplicate key.
func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: empty string", ErrInvalidURL)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if u.Scheme == "" {
		return "", fmt.Errorf("%w: missing scheme (use http or https)", ErrInvalidURL)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("%w: scheme must be http or https", ErrInvalidURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%w: missing host", ErrInvalidURL)
	}

	u.Scheme = scheme
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""

	if scheme == "http" && strings.HasSuffix(u.Host, ":80") {
		u.Host = strings.TrimSuffix(u.Host, ":80")
	}
	if scheme == "https" && strings.HasSuffix(u.Host, ":443") {
		u.Host = strings.TrimSuffix(u.Host, ":443")
	}

	if u.Path == "/" {
		u.Path = ""
	} else {
		u.Path = strings.TrimRight(u.Path, "/")
	}

	return u.String(), nil
}
