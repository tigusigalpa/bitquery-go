// OAuth2 client_credentials flow: mint + cache + auto-refresh tokens.
//
//	BITQUERY_CLIENT_ID=... BITQUERY_CLIENT_SECRET=... go run ./examples/oauth
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
	id, secret := os.Getenv("BITQUERY_CLIENT_ID"), os.Getenv("BITQUERY_CLIENT_SECRET")
	if id == "" || secret == "" {
		log.Fatal("set BITQUERY_CLIENT_ID and BITQUERY_CLIENT_SECRET")
	}

	provider := bitquery.NewClientCredentialsProvider(id, secret)

	client, err := v1.New(provider)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Execute(ctx, bitquery.Operation{
		Query: `query { ethereum { blocks { height } } }`,
	})
	if err != nil {
		log.Fatalf("execute: %v", err)
	}
	fmt.Println("status:", resp.StatusCode)
}
