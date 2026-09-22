package bitquery_test

// Opt-in live smoke tests. Disabled by default — they require a real
// token and must NEVER run in public CI. Enable explicitly:
//
//	BITQUERY_LIVE=1 BITQUERY_TOKEN=ory_at_... go test -run Live -v

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tigusigalpa/bitquery-go"
	"github.com/tigusigalpa/bitquery-go/subscription"
	v2 "github.com/tigusigalpa/bitquery-go/v2"
)

func liveToken(t *testing.T) string {
	t.Helper()
	if os.Getenv("BITQUERY_LIVE") != "1" {
		t.Skip("live tests disabled — set BITQUERY_LIVE=1")
	}
	tok := os.Getenv("BITQUERY_TOKEN")
	if tok == "" {
		t.Skip("BITQUERY_TOKEN not set")
	}
	return tok
}

func TestLiveV2Query(t *testing.T) {
	tok := liveToken(t)
	client, err := v2.New(bitquery.NewStaticTokenProvider(tok))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := client.Execute(ctx, bitquery.Operation{
		Query: `query { EVM(network: eth) { Blocks(limit: {count: 1}) { Block { Number } } } }`,
	})
	if err != nil {
		t.Fatalf("live query: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestLiveV2Subscription(t *testing.T) {
	tok := liveToken(t)
	client, err := subscription.New(bitquery.NewStaticTokenProvider(tok))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := client.Subscribe(ctx, bitquery.Operation{
		Query: `subscription { EVM(network: eth) { Blocks { Block { Number } } } }`,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	for ev := range stream.Events {
		if ev.Type == subscription.EventData {
			t.Log("live data received")
			return
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream: %v", err)
	}
}
