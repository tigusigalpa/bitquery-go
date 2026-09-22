package bitquery

import "testing"

func TestHTTPURLRegions(t *testing.T) {
	cases := []struct {
		version APIVersion
		region  Region
		want    string
	}{
		{V1, RegionEurope, V1EndpointEurope},
		{V1, RegionAsia, V1EndpointAsia},
		{V1, RegionUS, V1EndpointUS},
		{V1, "", V1EndpointEurope},   // default region
		{V1, "eu", V1EndpointEurope}, // alias
		{V2, RegionEurope, V2EndpointEurope},
		{V2, RegionAsia, V2EndpointAsia},
		{V2, RegionUS, V2EndpointUS},
	}
	for _, tc := range cases {
		got, err := HTTPURL(tc.version, tc.region, "")
		if err != nil {
			t.Fatalf("HTTPURL(%v,%v): %v", tc.version, tc.region, err)
		}
		if got != tc.want {
			t.Fatalf("HTTPURL(%v,%v) = %q, want %q", tc.version, tc.region, got, tc.want)
		}
	}
}

func TestHTTPURLOverrideWins(t *testing.T) {
	got, err := HTTPURL(V2, RegionUS, "https://example.test/graphql/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.test/graphql" {
		t.Fatalf("override = %q", got)
	}
}

func TestEndpointOverridesRejectCredentials(t *testing.T) {
	if _, err := HTTPURL(V2, RegionEurope, "https://example.test/graphql?token=secret"); err == nil {
		t.Fatal("expected sensitive HTTP endpoint query to be rejected")
	}
	if _, err := WebSocketURL(RegionEurope, "", "wss://example.test/graphql?token=secret"); err == nil {
		t.Fatal("expected sensitive WebSocket endpoint query to be rejected")
	}
}

func TestHTTPURLUnknownRegion(t *testing.T) {
	if _, err := HTTPURL(V1, Region("mars"), ""); err == nil {
		t.Fatal("expected region error")
	}
	if _, err := HTTPURL(V1, RegionUS, ""); err != nil {
		t.Fatal(err)
	}
}

func TestWebSocketURLDerivation(t *testing.T) {
	got, err := WebSocketURL(RegionEurope, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://streaming.bitquery.io/graphql" {
		t.Fatalf("ws = %q", got)
	}
}

func TestWebSocketURLOverrideWins(t *testing.T) {
	got, err := WebSocketURL(RegionAsia, "", "wss://ws.example.test/gql")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://ws.example.test/gql" {
		t.Fatalf("ws = %q", got)
	}
}

func TestParseAPIVersion(t *testing.T) {
	if v, err := ParseAPIVersion("v1"); err != nil || v != V1 {
		t.Fatalf("v1: %v %v", v, err)
	}
	if v, err := ParseAPIVersion("2"); err != nil || v != V2 {
		t.Fatalf("v2: %v %v", v, err)
	}
	if _, err := ParseAPIVersion("v3"); err == nil {
		t.Fatal("expected error for v3")
	}
}
