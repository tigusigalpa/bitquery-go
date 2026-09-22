package bitquery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ApiVersion identifies the Bitquery API contract. V1 and V2 are
// separate contracts — never interchangeable.
type ApiVersion int

const (
	// V1 is the historical GraphQL API.
	V1 ApiVersion = 1
	// V2 is the streaming GraphQL API (queries + subscriptions).
	V2 ApiVersion = 2
)

func (v ApiVersion) String() string {
	switch v {
	case V1:
		return "v1"
	case V2:
		return "v2"
	default:
		return fmt.Sprintf("ApiVersion(%d)", int(v))
	}
}

// ParseApiVersion accepts "v1"/"1" and "v2"/"2".
func ParseApiVersion(s string) (ApiVersion, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "v1", "1":
		return V1, nil
	case "v2", "2":
		return V2, nil
	default:
		return 0, &Error{Kind: KindConfig, Message: fmt.Sprintf("unknown Bitquery API version %q (expected v1 or v2)", s)}
	}
}

// Region selects the Bitquery regional endpoint family. It is a string
// type, not a closed enum, so future regions can pass through.
type Region string

const (
	RegionEurope Region = "europe"
	RegionAsia   Region = "asia"
	RegionUS     Region = "us"
)

func (r Region) normalized() (Region, error) {
	switch Region(strings.ToLower(strings.TrimSpace(string(r)))) {
	case "", RegionEurope, "eu", "default":
		return RegionEurope, nil
	case RegionAsia:
		return RegionAsia, nil
	case RegionUS, "usa", "united-states":
		return RegionUS, nil
	default:
		return "", &Error{Kind: KindConfig, Message: fmt.Sprintf("unknown Bitquery region %q (expected europe, asia or us)", r)}
	}
}

// Network is a blockchain network identifier. Deliberately an open
// string type — the supported set depends on region, plan, cube and
// dataset and evolves over time.
type Network string

// Common networks — convenience only, not an exhaustive list.
const (
	NetworkEthereum Network = "eth"
	NetworkBSC      Network = "bsc"
	NetworkMatic    Network = "matic"
	NetworkTron     Network = "tron"
	NetworkSolana   Network = "solana"
	NetworkArbitrum Network = "arbitrum"
	NetworkOptimism Network = "optimism"
	NetworkBase     Network = "base"
)

// Operation is a GraphQL document plus variables. Values always travel
// as variables — never interpolate user input into the document.
type Operation struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables,omitempty"`
	OperationName string         `json:"operationName,omitempty"`
}

// GraphQLError is one entry of a GraphQL errors[] array.
type GraphQLError struct {
	Message    string           `json:"message"`
	Locations  []map[string]any `json:"locations,omitempty"`
	Path       []any            `json:"path,omitempty"`
	Extensions map[string]any   `json:"extensions,omitempty"`
	Raw        json.RawMessage  `json:"-"`
}

func (e GraphQLError) Error() string { return e.Message }

// Response is the parsed GraphQL response plus HTTP metadata and the raw
// body. Data stays json.RawMessage so monetary values, decimals and
// large integers are never silently converted to float64 — decode with
// DecodeData (json.Number) or handle RawMessage directly.
type Response struct {
	Data       json.RawMessage `json:"-"`
	Errors     []GraphQLError  `json:"-"`
	Extensions json.RawMessage `json:"-"`
	StatusCode int             `json:"-"`
	Header     map[string][]string
	RawBody    []byte `json:"-"`
}

// HasErrors reports whether errors[] is non-empty.
func (r *Response) HasErrors() bool { return len(r.Errors) > 0 }

// HasPartialData reports a GraphQL partial response: data AND errors[].
func (r *Response) HasPartialData() bool { return r.Data != nil && len(r.Errors) > 0 }

// DecodeData unmarshals Data into v. Numbers inside interface{} targets
// decode as json.Number (never float64), preserving precision.
func (r *Response) DecodeData(v any) error {
	if r.Data == nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(r.Data))
	dec.UseNumber()
	return dec.Decode(v)
}
