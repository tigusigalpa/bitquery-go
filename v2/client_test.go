package v2

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tigusigalpa/bitquery-go"
)

func TestClientExecutesAndPreservesStrictResponse(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("Authorization = %q", got)
		}
		if calls == 1 {
			_, _ = w.Write([]byte(`{"data":{"EVM":{"ok":true}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"EVM":null},"errors":[{"message":"field unavailable"}]}`))
	}))
	defer server.Close()

	client, err := New(
		bitquery.NewStaticTokenProvider("token"),
		bitquery.WithBaseURL(server.URL),
		bitquery.WithRetryPolicy(bitquery.NoRetry()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint, err := client.Endpoint(); err != nil || endpoint != server.URL {
		t.Fatalf("Endpoint() = %q, %v", endpoint, err)
	}

	response, err := client.Execute(context.Background(), bitquery.Operation{Query: "{ EVM { ok } }"})
	if err != nil || response.StatusCode != http.StatusOK || response.HasErrors() {
		t.Fatalf("Execute() = %#v, %v", response, err)
	}

	response, err = client.ExecuteStrict(context.Background(), bitquery.Operation{Query: "{ EVM { unavailable } }"})
	if !errors.Is(err, bitquery.ErrGraphQL) || response == nil || !response.HasErrors() {
		t.Fatalf("ExecuteStrict() = %#v, %v", response, err)
	}
}
