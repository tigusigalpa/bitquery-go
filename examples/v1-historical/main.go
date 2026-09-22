// V1 historical GraphQL with an explicit V1 client and deprecation signal.
//
//	BITQUERY_TOKEN=... go run ./examples/v1-historical
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/tigusigalpa/bitquery-go"
	v1 "github.com/tigusigalpa/bitquery-go/v1"
)

func main() {
	token := os.Getenv("BITQUERY_TOKEN")
	if token == "" {
		log.Fatal("set BITQUERY_TOKEN")
	}

	client, err := v1.New(bitquery.NewStaticTokenProvider(token))
	if err != nil {
		log.Fatal(err)
	}

	op := v1.Blocks(bitquery.NetworkEthereum, 10, "", "")
	for _, notice := range client.DeprecationNotices(op) {
		log.Printf("V1 notice for %s: %s", notice.Network, notice.Message)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := client.Execute(ctx, op)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(resp.Data))
}
