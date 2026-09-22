// Region, endpoint and transport configuration for an HTTP V2 client.
//
//	BITQUERY_TOKEN=... go run ./examples/custom-endpoint
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/tigusigalpa/bitquery-go"
	v2 "github.com/tigusigalpa/bitquery-go/v2"
)

func main() {
	token := os.Getenv("BITQUERY_TOKEN")
	if token == "" {
		log.Fatal("set BITQUERY_TOKEN")
	}

	client, err := v2.New(
		bitquery.NewStaticTokenProvider(token),
		bitquery.WithRegion(bitquery.RegionUS),
		// WithBaseURL wins over WithRegion. Use it only for a trusted proxy
		// or test server; do not include credentials in the URL.
		// bitquery.WithBaseURL("https://proxy.example.com/graphql"),
		bitquery.WithTimeout(20*time.Second),
		bitquery.WithUserAgent("my-bitquery-worker/1.0"),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp, err := client.Execute(ctx, bitquery.Operation{
		Query: `query { EVM(network: eth) { Blocks(limit: {count: 1}) { Block { Number } } } }`,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(resp.Data))
}
