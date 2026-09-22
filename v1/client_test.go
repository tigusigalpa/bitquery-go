package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
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
