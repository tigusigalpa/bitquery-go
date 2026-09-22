package redact

import (
	"strings"
	"testing"
)

func TestStringRedactsTokenParam(t *testing.T) {
	got := String("dial wss://x.test/graphql?token=SECRET123&foo=1 failed")
	if strings.Contains(got, "SECRET123") {
		t.Fatalf("leak: %s", got)
	}
	if !strings.Contains(got, "token="+Placeholder) {
		t.Fatalf("no marker: %s", got)
	}
}

func TestStringRedactsBearerAndJSONFields(t *testing.T) {
	got := String(`Authorization: Bearer abc.def.ghi {"access_token":"XYZ","client_secret":"S"}`)
	if strings.Contains(got, "abc.def.ghi") || strings.Contains(got, `"XYZ"`) || strings.Contains(got, `"S"`) {
		t.Fatalf("leak: %s", got)
	}
}

func TestURLQuery(t *testing.T) {
	got := URLQuery("wss://x.test/graphql?token=T&other=1")
	if strings.Contains(got, "token=T") || strings.Contains(got, "=T&") {
		t.Fatalf("leak: %s", got)
	}
	if !strings.Contains(got, "other=1") {
		t.Fatalf("non-secret param lost: %s", got)
	}
}

func TestMapAndHeaders(t *testing.T) {
	m := Map(map[string]any{
		"authorization": "Bearer x",
		"nested":        map[string]any{"token": "y"},
		"safe":          "v",
	})
	if m["authorization"] != Placeholder {
		t.Fatal("header not redacted")
	}
	if m["nested"].(map[string]any)["token"] != Placeholder {
		t.Fatal("nested token not redacted")
	}
	if m["safe"] != "v" {
		t.Fatal("safe value clobbered")
	}

	h := Headers(map[string][]string{
		"Authorization": {"Bearer x"},
		"X-Other":       {"v"},
	})
	if h["Authorization"][0] != Placeholder || h["X-Other"][0] != "v" {
		t.Fatalf("headers: %v", h)
	}
}
