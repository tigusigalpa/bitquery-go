package subscription

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tigusigalpa/bitquery-go/v2"
)

func TestAcknowledgedDropResetsReconnectBudget(t *testing.T) {
	for _, protocol := range []bitquery.SubProtocol{
		bitquery.SubProtocolGraphQLTransportWS,
		bitquery.SubProtocolGraphQLWS,
	} {
		t.Run(string(protocol), func(t *testing.T) {
			d := &dialRec{}
			var connection atomic.Int32
			d.serve = func(conn *fakeConn) {
				switch connection.Add(1) {
				case 1:
					conn.waitWrite(1)
					conn.pushErr(errors.New("dropped before acknowledgement"))
				case 2:
					serveHandshake(conn, func() {
						conn.push(map[string]any{"type": protocol.DataType(), "id": "1", "payload": map[string]any{"data": 2}})
						conn.pushErr(errors.New("dropped after acknowledgement"))
					})
				case 3:
					serveHandshake(conn, func() {
						conn.push(map[string]any{"type": protocol.DataType(), "id": "1", "payload": map[string]any{"data": 3}})
						conn.push(map[string]any{"type": "complete", "id": "1"})
					})
				default:
					t.Errorf("unexpected connection %d", connection.Load())
				}
			}
			client := newTestClient(t, d,
				bitquery.WithSubProtocol(protocol),
				bitquery.WithSubscriptionReconnect(1),
			)

			stream, err := client.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
			if err != nil {
				t.Fatal(err)
			}
			events := drainEvents(stream)
			if err := stream.Err(); err != nil {
				t.Fatalf("stream error = %v", err)
			}
			if got := d.count(); got != 3 {
				t.Fatalf("dials = %d, want 3", got)
			}

			var data []Event
			for _, event := range events {
				if event.Type == EventData {
					data = append(data, event)
				}
			}
			if len(data) != 2 {
				t.Fatalf("data events = %d, want 2", len(data))
			}
			if data[0].Delivery.ConnectionEpoch != 2 || !data[0].Delivery.ReconnectGap || data[1].Delivery.ConnectionEpoch != 3 || !data[1].Delivery.ReconnectGap {
				t.Fatalf("reconnected delivery metadata = %#v", data)
			}
			if data[1].Delivery.ReceiveSequence <= data[0].Delivery.ReceiveSequence || data[0].Delivery.ReceivedAt.IsZero() {
				t.Fatalf("delivery sequence/time = %#v", data)
			}
		})
	}
}

func TestPreAcknowledgementDropsStillUseReconnectBudget(t *testing.T) {
	for _, protocol := range []bitquery.SubProtocol{
		bitquery.SubProtocolGraphQLTransportWS,
		bitquery.SubProtocolGraphQLWS,
	} {
		t.Run(string(protocol), func(t *testing.T) {
			d := &dialRec{}
			d.serve = func(conn *fakeConn) {
				conn.waitWrite(1)
				conn.pushErr(errors.New("dropped before acknowledgement"))
			}
			client := newTestClient(t, d,
				bitquery.WithSubProtocol(protocol),
				bitquery.WithSubscriptionReconnect(1),
			)

			stream, err := client.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
			if err != nil {
				t.Fatal(err)
			}
			stream.Wait()
			if stream.Err() == nil {
				t.Fatal("expected reconnect budget exhaustion")
			}
			if got := d.count(); got != 2 {
				t.Fatalf("dials = %d, want initial plus one retry", got)
			}
		})
	}
}

func TestCompleteOverflowFailsClosedAndRetainsEvidence(t *testing.T) {
	for _, protocol := range []bitquery.SubProtocol{
		bitquery.SubProtocolGraphQLTransportWS,
		bitquery.SubProtocolGraphQLWS,
	} {
		t.Run(string(protocol), func(t *testing.T) {
			d := &dialRec{}
			d.serve = func(conn *fakeConn) {
				serveHandshake(conn, func() {
					conn.push(map[string]any{"type": protocol.DataType(), "id": "1", "payload": map[string]any{"data": 1}})
					conn.push(map[string]any{"type": "complete", "id": "1"})
				})
			}
			client := newTestClient(t, d,
				bitquery.WithSubProtocol(protocol),
				bitquery.WithSubscriptionQueue(1, bitquery.OverflowFail),
				bitquery.WithSubscriptionReconnect(1),
			)

			stream, err := client.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
			if err != nil {
				t.Fatal(err)
			}
			stream.Wait()
			err = stream.Err()
			if err == nil || !strings.Contains(err.Error(), "overflow") {
				t.Fatalf("complete overflow must be terminal, got %v", err)
			}
			var apiErr *bitquery.Error
			if !errors.As(err, &apiErr) || len(apiErr.Receipts) != 1 || frameType(apiErr.Receipts[0].Raw()) != "complete" {
				t.Fatalf("complete overflow evidence = %#v", apiErr)
			}
			gaps := stream.Gaps()
			if len(gaps) != 1 || gaps[0].Reason != GapOverflowFail {
				t.Fatalf("overflow gaps = %#v", gaps)
			}
			if got := d.count(); got != 1 {
				t.Fatalf("terminal overflow must not reconnect; dials=%d", got)
			}
		})
	}
}

func TestCompleteUsesFreeQueueAndDropOldestSemantics(t *testing.T) {
	for _, policy := range []string{bitquery.OverflowFail, bitquery.OverflowDropOldest} {
		for _, protocol := range []bitquery.SubProtocol{
			bitquery.SubProtocolGraphQLTransportWS,
			bitquery.SubProtocolGraphQLWS,
		} {
			name := string(protocol) + "/" + policy
			t.Run(name, func(t *testing.T) {
				d := &dialRec{}
				d.serve = func(conn *fakeConn) {
					serveHandshake(conn, func() {
						conn.push(map[string]any{"type": protocol.DataType(), "id": "1", "payload": json.RawMessage(`{"data":1}`)})
						conn.push(map[string]any{"type": "complete", "id": "1"})
					})
				}
				capacity := 2
				if policy == bitquery.OverflowDropOldest {
					capacity = 1
				}
				client := newTestClient(t, d,
					bitquery.WithSubProtocol(protocol),
					bitquery.WithSubscriptionQueue(capacity, policy),
				)
				stream, err := client.Subscribe(context.Background(), bitquery.Operation{Query: "subscription { x }"})
				if err != nil {
					t.Fatal(err)
				}
				events := drainEvents(stream)
				if err := stream.Err(); err != nil {
					t.Fatalf("free/drop_oldest complete must be clean: %v", err)
				}
				if got := d.count(); got != 1 {
					t.Fatalf("complete must not reconnect; dials=%d", got)
				}
				if events[len(events)-1].Type != EventComplete {
					t.Fatalf("last event = %#v, want complete", events)
				}
			})
		}
	}
}
