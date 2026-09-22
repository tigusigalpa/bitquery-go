package bitquery_test

import (
	"context"
	"fmt"
	"os"

	"github.com/tigusigalpa/bitquery-go"
	v1 "github.com/tigusigalpa/bitquery-go/v1"
	v2 "github.com/tigusigalpa/bitquery-go/v2"
)

// Example_v1 — the V1 historical GraphQL client (HTTPS only).
func Example_v1() {
	client, err := v1.New(bitquery.NewStaticTokenProvider(os.Getenv("BITQUERY_TOKEN")))
	if err != nil {
		panic(err)
	}
	resp, err := client.Execute(context.Background(), bitquery.Operation{
		Query: `query { ethereum { blocks { height } } }`,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(resp.StatusCode)
}

// Example_v2 — the V2 streaming GraphQL client over HTTPS.
func Example_v2() {
	client, err := v2.New(
		bitquery.NewClientCredentialsProvider(
			os.Getenv("BITQUERY_CLIENT_ID"),
			os.Getenv("BITQUERY_CLIENT_SECRET"),
		),
		bitquery.WithRegion(bitquery.RegionUS),
	)
	if err != nil {
		panic(err)
	}
	resp, err := client.Execute(context.Background(), bitquery.Operation{
		Query: `query { EVM(network: eth) { Blocks(limit: {count: 1}) { Block { Number } } } }`,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(resp.HasErrors())
}
