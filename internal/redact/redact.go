// Package redact scrubs credentials from every observability path:
// Authorization headers, Bearer tokens, OAuth access_token /
// client_secret fields and the `token` URL query parameter used by the
// Bitquery WebSocket endpoint.
package redact

import (
	"net/url"
	"regexp"
	"strings"
)

// Placeholder replaces every secret fragment.
const Placeholder = "[REDACTED]"

var (
	tokenParamRe  = regexp.MustCompile(`(?i)([?&]token=)[^&\s"']+`)
	bearerRe      = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9_\-\.]{4,}`)
	authHeaderRe  = regexp.MustCompile(`(?i)(Authorization["']?\s*[:=]\s*["']?\s*Bearer\s+)[^"'\s,}]+`)
	jsonFieldRe   = regexp.MustCompile(`(?i)("(?:access_token|client_secret|refresh_token|token)"\s*:\s*")[^"]+`)
	formFieldRe   = regexp.MustCompile(`(?i)((?:access_token|client_secret|refresh_token)=)[^&\s"']+`)
	sensitiveKeys = map[string]bool{
		"authorization": true, "x-api-key": true,
		"access_token": true, "client_secret": true, "refresh_token": true,
		"token": true, "secret": true, "password": true,
	}
)

// String sanitizes a free-form string (message, URL, header dump).
func String(s string) string {
	s = tokenParamRe.ReplaceAllString(s, "${1}"+Placeholder)
	s = authHeaderRe.ReplaceAllString(s, "${1}"+Placeholder)
	s = bearerRe.ReplaceAllString(s, "${1}"+Placeholder)
	s = jsonFieldRe.ReplaceAllString(s, "${1}"+Placeholder)
	return formFieldRe.ReplaceAllString(s, "${1}"+Placeholder)
}

// URL sanitizes a URL, redacting the token query parameter.
func URL(raw string) string {
	return String(raw)
}

// URLQuery redacts the token param by rebuilding the query. Falls back
// to pattern redaction on parse failure.
func URLQuery(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return String(raw)
	}
	q := u.Query()
	if q.Get("token") != "" {
		q.Set("token", Placeholder)
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// Map sanitizes a context map recursively.
func Map(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch {
		case sensitiveKeys[strings.ToLower(k)]:
			out[k] = Placeholder
		default:
			switch vv := v.(type) {
			case string:
				out[k] = String(vv)
			case map[string]any:
				out[k] = Map(vv)
			default:
				out[k] = v
			}
		}
	}
	return out
}

// Headers sanitizes HTTP headers for logging.
func Headers(h map[string][]string) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		if sensitiveKeys[strings.ToLower(k)] {
			out[k] = []string{Placeholder}
			continue
		}
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}
