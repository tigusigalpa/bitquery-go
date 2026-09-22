// V2 WebSocket subscription — opt-in client, separate from the HTTP
// client. Run as a worker (never inside a short-lived request). Press
// Ctrl+C to close the socket — the only way to end a Bitquery stream.
//
//	BITQUERY_TOKEN=... go run ./examples/subscription
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/tigusigalpa/bitquery-go"
	"github.com/tigusigalpa/bitquery-go/subscription"
)

func main() {
	token := os.Getenv("BITQUERY_TOKEN")
	if token == "" {
		log.Fatal("set BITQUERY_TOKEN")
	}

	client, err := subscription.New(
		bitquery.NewStaticTokenProvider(token),
		bitquery.WithSubProtocol(bitquery.SubProtocolGraphQLTransportWS),
		bitquery.WithSubscriptionQueue(500, bitquery.OverflowDropOldest),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	stream, err := client.Subscribe(ctx, bitquery.Operation{
		Query: `subscription {
			EVM(network: eth) {
				Blocks { Block { Number Time } }
			}
		}`,
	})
	if err != nil {
		log.Fatal(err)
	}

	for ev := range stream.Events {
		switch ev.Type {
		case subscription.EventData:
			// At-least-once, unordered across block portions —
			// deduplicate downstream before persisting.
			fmt.Println("data:", string(ev.Payload))
		case subscription.EventKeepalive:
			// server keepalive — no action needed
		case subscription.EventComplete:
			log.Println("stream complete")
		}
	}

	if err := stream.Err(); err != nil {
		log.Fatalf("stream terminated: %v", err)
	}
	log.Printf("dropped events: %d", stream.Dropped())
}
