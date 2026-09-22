// HTTP query against the V2 streaming GraphQL endpoint.
//
//	go run ./examples/http            # needs BITQUERY_TOKEN
//	BITQUERY_REGION=us go run ./examples/http
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
		bitquery.WithRegion(bitquery.Region(os.Getenv("BITQUERY_REGION"))),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Execute(ctx, bitquery.Operation{
		Query: `query {
			EVM(network: eth) {
				Blocks(limit: {count: 3}) {
					Block { Number Time }
				}
			}
		}`,
	})
	if err != nil {
		log.Fatalf("execute: %v", err)
	}
	if resp.HasErrors() {
		for _, ge := range resp.Errors {
			log.Printf("graphql error: %s", ge.Message)
		}
		if resp.HasPartialData() {
			log.Println("partial data present — still decoding")
		}
	}

	var data map[string]any
	if err := resp.DecodeData(&data); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%v\n", data)
}
