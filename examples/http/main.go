// HTTP query against the V2 streaming GraphQL endpoint.
//
//	go run ./examples/http            # needs BITQUERY_TOKEN
//	BITQUERY_REGION=us go run ./examples/http
package main

import (
	"context"
	"errors"
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
		var apiErr *bitquery.Error
		if errors.As(err, &apiErr) {
			logHTTPReceipts(apiErr.Receipts)
		}
		log.Fatalf("execute: %v", err)
	}
	logHTTPReceipts(resp.Receipts)
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

func logHTTPReceipts(receipts []bitquery.Receipt) {
	for _, receipt := range receipts {
		if !receipt.HTTPBody.Applicable {
			continue
		}
		log.Printf(
			"http receipt status=%d complete=%t read_complete=%t read_error=%t limit_exceeded=%t close_error=%t operation_sha256=%s",
			receipt.StatusCode,
			receipt.HTTPBody.Complete,
			receipt.HTTPBody.ReadComplete,
			receipt.HTTPBody.ReadError,
			receipt.HTTPBody.LimitExceeded,
			receipt.HTTPBody.CloseError,
			receipt.OperationSHA256,
		)
	}
}
