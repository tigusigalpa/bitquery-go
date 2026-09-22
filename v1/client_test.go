package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/tigusigalpa/bitquery-go"
)

func TestDeprecationNotices(t *testing.T) {
	for _, net := range []string{"ethereum", "eth", "bsc", "matic", "tron", "Ethereum", " BSC "} {
		if n := NoticesForNetwork(net); len(n) != 1 {
			t.Fatalf("network %s: expected notice", net)
		}
	}
	if n := NoticesForNetwork("solana"); n != nil {
		t.Fatal("solana is not deprecated in V1")
	}
}

func TestBlocksWithoutDatesOmitsEmptyDateFilter(t *testing.T) {
	op := Blocks(bitquery.NetworkEthereum, 10, "", "")
	if strings.Contains(op.Query, "date:") {
		t.Fatalf("unbounded block query contains a null date filter: %s", op.Query)
	}
	if len(op.Variables) != 2 {
		t.Fatalf("variables = %v", op.Variables)
	}
}

func TestV1QueryFactoriesKeepValuesInVariables(t *testing.T) {
	const (
		txHash   = "0xdeadbeef"
		address  = "0x1234567890abcdef"
		exchange = "Uniswap"
		contract = "0xfeedface"
	)
	tests := []struct {
		name      string
		op        bitquery.Operation
		wantField string
		wantVars  map[string]any
		forbidden []string
	}{
		{
			name:      "blocks with dates",
			op:        Blocks(bitquery.Network("dynamic-network"), 10, "2024-01-01", "2024-01-02"),
			wantField: "blocks",
			wantVars:  map[string]any{"network": "dynamic-network", "limit": 10, "from": "2024-01-01", "till": "2024-01-02"},
			forbidden: []string{"dynamic-network", "2024-01-01", "2024-01-02"},
		},
		{
			name:      "transactions",
			op:        Transactions(bitquery.Network("dynamic-network"), 20, txHash),
			wantField: "transactions",
			wantVars:  map[string]any{"network": "dynamic-network", "limit": 20, "hash": txHash},
			forbidden: []string{"dynamic-network", txHash},
		},
		{
			name:      "transfers",
			op:        Transfers(bitquery.Network("dynamic-network"), 30, address),
			wantField: "transfers",
			wantVars:  map[string]any{"network": "dynamic-network", "limit": 30, "address": address},
			forbidden: []string{"dynamic-network", address},
		},
		{
			name:      "DEX trades",
			op:        DexTrades(bitquery.Network("dynamic-network"), 40, exchange),
			wantField: "dexTrades",
			wantVars:  map[string]any{"network": "dynamic-network", "limit": 40, "exchange": exchange},
			forbidden: []string{"dynamic-network", exchange},
		},
		{
			name:      "smart contract calls",
			op:        SmartContractCalls(bitquery.Network("dynamic-network"), 50, contract, "2024-01-01", "2024-01-02"),
			wantField: "smartContractCalls",
			wantVars:  map[string]any{"network": "dynamic-network", "limit": 50, "contract": contract, "from": "2024-01-01", "till": "2024-01-02"},
			forbidden: []string{"dynamic-network", contract, "2024-01-01", "2024-01-02"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !strings.Contains(test.op.Query, test.wantField) {
				t.Fatalf("query does not select %q", test.wantField)
			}
			if !reflect.DeepEqual(test.op.Variables, test.wantVars) {
				t.Fatalf("variables = %#v, want %#v", test.op.Variables, test.wantVars)
			}
			for key := range test.wantVars {
				if !strings.Contains(test.op.Query, "$"+key) {
					t.Fatalf("query does not reference variable $%s", key)
				}
			}
			for _, value := range test.forbidden {
				if strings.Contains(test.op.Query, value) {
					t.Fatalf("query interpolates dynamic value %q", value)
				}
			}
		})
	}
}

func TestDeprecationNoticesFromVariables(t *testing.T) {
	c := &Client{}
	op := bitquery.Operation{Variables: map[string]any{
		"network": "ethereum",
		"nested":  map[string]any{"network": "solana"},
	}}
	notices := c.DeprecationNotices(op)
	if len(notices) != 1 || notices[0].Network != "ethereum" {
		t.Fatalf("notices = %+v", notices)
	}
}

func TestExecuteThroughHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"ethereum":{"block":1}}}`))
	}))
	defer srv.Close()

	c, err := New(
		bitquery.NewStaticTokenProvider("T"),
		bitquery.WithBaseURL(srv.URL),
	)
	if err != nil {
		t.Fatal(err)
	}
	if ep, _ := c.Endpoint(); ep != srv.URL {
		t.Fatalf("endpoint = %q", ep)
	}
	resp, err := c.Execute(context.Background(), bitquery.Operation{Query: "{ ethereum { block } }"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
