// V2 Solana HTTP query for one token mint.
//
//	BITQUERY_TOKEN=... SOLANA_MINT=... go run ./examples/v2-solana
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
	mint := os.Getenv("SOLANA_MINT")
	if mint == "" {
		log.Fatal("set SOLANA_MINT")
	}

	client, err := v2.New(bitquery.NewStaticTokenProvider(token))
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := client.Execute(ctx, v2.SolanaTransfers(20, mint))
	if err != nil {
		log.Fatal(err)
	}
	if resp.HasErrors() {
		for _, graphQLError := range resp.Errors {
			log.Printf("GraphQL: %s", graphQLError.Message)
		}
	}
	fmt.Println(string(resp.Data))
}
