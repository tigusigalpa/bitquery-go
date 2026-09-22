package bitquery

import (
	"encoding/json"
	"testing"
)

func TestAPIVersionStringAndRegionAliases(t *testing.T) {
	if got := APIVersion(99).String(); got != "APIVersion(99)" {
		t.Fatalf("unknown API version = %q", got)
	}
	for _, test := range []struct {
		in   Region
		want Region
	}{{"", RegionEurope}, {"EU", RegionEurope}, {"united-states", RegionUS}} {
		got, err := test.in.normalized()
		if err != nil || got != test.want {
			t.Fatalf("normalized(%q) = %q, %v; want %q", test.in, got, err, test.want)
		}
	}
}

func TestResponseDecodeDataUsesJSONNumber(t *testing.T) {
	response := &Response{Data: json.RawMessage(`{"large":9007199254740993}`)}
	var decoded map[string]any
	if err := response.DecodeData(&decoded); err != nil {
		t.Fatal(err)
	}
	if got, ok := decoded["large"].(json.Number); !ok || got.String() != "9007199254740993" {
		t.Fatalf("large = %#v, want exact json.Number", decoded["large"])
	}
	if err := (&Response{}).DecodeData(&decoded); err != nil {
		t.Fatalf("empty data = %v", err)
	}
}

func TestGraphQLErrorError(t *testing.T) {
	if got := (GraphQLError{Message: "unknown field"}).Error(); got != "unknown field" {
		t.Fatalf("Error() = %q", got)
	}
}
