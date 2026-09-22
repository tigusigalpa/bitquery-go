package bitquery

import (
	"net/url"
	"strings"
)

// Default regional endpoints. All are overridable via Config options.
//
// https://docs.bitquery.io/docs/start/endpoints/
const (
	V1EndpointEurope = "https://graphql.bitquery.io"
	V1EndpointAsia   = "https://asia.graphql.bitquery.io"
	V1EndpointUS     = "https://us.graphql.bitquery.io"

	V2EndpointEurope = "https://streaming.bitquery.io/graphql"
	V2EndpointAsia   = "https://asia.streaming.bitquery.io/graphql"
	V2EndpointUS     = "https://us.streaming.bitquery.io/graphql"

	// OAuthTokenEndpoint issues OAuth2 access tokens (all regions).
	OAuthTokenEndpoint = "https://oauth2.bitquery.io/oauth2/token" // #nosec G101 -- public endpoint, not a credential.
)

var v1Endpoints = map[Region]string{
	RegionEurope: V1EndpointEurope,
	RegionAsia:   V1EndpointAsia,
	RegionUS:     V1EndpointUS,
}

var v2Endpoints = map[Region]string{
	RegionEurope: V2EndpointEurope,
	RegionAsia:   V2EndpointAsia,
	RegionUS:     V2EndpointUS,
}

// HTTPURL resolves the GraphQL HTTPS endpoint for a version/region.
// An explicit override always wins over the region default.
func HTTPURL(version APIVersion, region Region, override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		if err := validateHTTPURL(override, "HTTP endpoint"); err != nil {
			return "", err
		}
		return strings.TrimRight(override, "/"), nil
	}

	norm, err := region.normalized()
	if err != nil {
		return "", err
	}

	if version == V1 {
		return v1Endpoints[norm], nil
	}
	if version == V2 {
		return v2Endpoints[norm], nil
	}

	return "", &Error{Kind: KindConfig, Message: "API version must be V1 or V2"}
}

// WebSocketURL resolves the V2 subscription endpoint. An explicit WS
// override wins; otherwise the V2 HTTPS endpoint with https→wss.
func WebSocketURL(region Region, httpsOverride, wsOverride string) (string, error) {
	if strings.TrimSpace(wsOverride) != "" {
		if err := validateWebSocketURL(wsOverride); err != nil {
			return "", err
		}
		return strings.TrimRight(wsOverride, "/"), nil
	}

	https, err := HTTPURL(V2, region, httpsOverride)
	if err != nil {
		return "", err
	}

	switch {
	case strings.HasPrefix(https, "https://"):
		return "wss://" + https[len("https://"):], nil
	case strings.HasPrefix(https, "http://"):
		return "ws://" + https[len("http://"):], nil
	default:
		return "", &Error{Kind: KindConfig, Message: "cannot derive a WebSocket endpoint from " + https + " — provide an explicit ws endpoint"}
	}
}

func validateHTTPURL(raw, label string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return &Error{Kind: KindConfig, Message: label + " must be an absolute http(s) URL"}
	}
	if u.User != nil {
		return &Error{Kind: KindConfig, Message: label + " must not contain user credentials"}
	}
	if hasSensitiveQuery(u) {
		return &Error{Kind: KindConfig, Message: label + " must not contain credentials in its query string"}
	}
	return nil
}

func validateWebSocketURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "wss" && u.Scheme != "ws") {
		return &Error{Kind: KindConfig, Message: "WebSocket endpoint must be an absolute ws(s) URL"}
	}
	if u.User != nil {
		return &Error{Kind: KindConfig, Message: "WebSocket endpoint must not contain user credentials"}
	}
	if hasSensitiveQuery(u) {
		return &Error{Kind: KindConfig, Message: "WebSocket endpoint must not contain credentials in its query string"}
	}
	return nil
}

func hasSensitiveQuery(u *url.URL) bool {
	for key := range u.Query() {
		switch strings.ToLower(key) {
		case "token", "access_token", "client_secret", "authorization":
			return true
		}
	}
	return false
}
