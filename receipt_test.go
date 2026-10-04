package bitquery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOptionalPreservesAbsentNullAndZero(t *testing.T) {
	variables := map[string]any{}
	var absent Optional[int]
	absent.SetVariable(variables, "absent")
	Null[int]().SetVariable(variables, "null")
	Present(0).SetVariable(variables, "zero")

	if _, ok := variables["absent"]; ok {
		t.Fatal("absent optional must not add a variable")
	}
	if value, ok := variables["null"]; !ok || value != nil {
		t.Fatalf("null = %#v, present = %v", value, ok)
	}
	if value, ok := variables["zero"].(int); !ok || value != 0 {
		t.Fatalf("zero = %#v", variables["zero"])
	}
}

func TestReceiptSnapshotsRawOperationAndFrame(t *testing.T) {
	operation := []byte(`{"query":"query { x }","variables":{"n":0}}`)
	raw := []byte(`raw-frame`)
	capturedAt := time.Date(2026, time.October, 3, 10, 11, 12, 0, time.FixedZone("MSK", 3*60*60))
	receipt := NewReceipt(
		ReceiptSourceWebSocket,
		ReceiptReceived,
		"wss://ws.example.test/graphql?token=SECRET",
		operation,
		raw,
		0,
		capturedAt,
	)

	operation[0] = '!'
	raw[0] = '!'
	if got := string(receipt.Operation()); got != `{"query":"query { x }","variables":{"n":0}}` {
		t.Fatalf("Operation() = %q", got)
	}
	if got := string(receipt.Raw()); got != "raw-frame" {
		t.Fatalf("Raw() = %q", got)
	}
	copy := receipt.Raw()
	copy[0] = '!'
	if got := string(receipt.Raw()); got != "raw-frame" {
		t.Fatalf("Raw() changed through returned copy: %q", got)
	}
	if strings.Contains(receipt.Target, "SECRET") || !strings.Contains(receipt.Target, "REDACTED") {
		t.Fatalf("target was not redacted: %q", receipt.Target)
	}
	if receipt.CapturedAt.Location() != time.UTC || len(receipt.OperationSHA256) != 64 {
		t.Fatalf("receipt metadata = %+v", receipt)
	}
	if receipt.HTTPBody.Applicable {
		t.Fatalf("WebSocket receipt must not invent HTTP body evidence: %+v", receipt.HTTPBody)
	}
}

func TestHTTPReceiptsCaptureEveryRetryResponse(t *testing.T) {
	var calls int
	firstBody := []byte(`temporary failure`)
	finalBody := []byte(`{"data":{"ok":true,"unknown":{"still":"raw"}}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write(firstBody)
			return
		}
		_, _ = w.Write(finalBody)
	}))
	defer server.Close()

	op := Operation{Query: "query RetryReceipt($n: Int!) { value(n: $n) }", Variables: map[string]any{"n": 0}}
	wantOperation, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	executor := newTestExecutor(t, V2, server.URL)
	response, err := executor.Execute(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	// Mutating the caller-owned map after Execute cannot alter the receipts.
	op.Variables["n"] = 99

	if len(response.Receipts) != 2 {
		t.Fatalf("receipts = %d, want 2", len(response.Receipts))
	}
	for index, receipt := range response.Receipts {
		if receipt.Source != ReceiptSourceHTTP || receipt.Direction != ReceiptReceived || receipt.StatusCode == 0 || receipt.CapturedAt.IsZero() {
			t.Fatalf("receipt[%d] metadata = %+v", index, receipt)
		}
		if got := receipt.Operation(); !bytes.Equal(got, wantOperation) {
			t.Fatalf("receipt[%d] operation = %s, want %s", index, got, wantOperation)
		}
	}
	if got := response.Receipts[0].Raw(); !bytes.Equal(got, firstBody) {
		t.Fatalf("first raw body = %q", got)
	}
	if got := response.Receipts[1].Raw(); !bytes.Equal(got, finalBody) {
		t.Fatalf("final raw body = %q", got)
	}
	if got := response.RawBody; !bytes.Equal(got, finalBody) {
		t.Fatalf("response raw body = %q", got)
	}
	var decoded map[string]any
	if err := response.DecodeData(&decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["unknown"].(map[string]any); !ok {
		t.Fatalf("unknown response key was not preserved: %#v", decoded)
	}
}

func TestHTTPErrorRetainsReceipt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`denied`))
	}))
	defer server.Close()

	executor := newTestExecutor(t, V2, server.URL, WithRetryPolicy(NoRetry()))
	_, err := executor.Execute(context.Background(), Operation{Query: "{ denied }"})
	var apiErr *Error
	if !errors.As(err, &apiErr) || len(apiErr.Receipts) != 1 {
		t.Fatalf("error = %v, receipts = %#v", err, apiErr)
	}
	if got := string(apiErr.Receipts[0].Raw()); got != "denied" {
		t.Fatalf("raw error body = %q", got)
	}
}

func TestHTTPReceiptsSurviveLaterTransportFailure(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("retryable response")),
			}, nil
		}
		return nil, errors.New("connection reset")
	})}
	executor, err := NewExecutor(
		V2,
		WithTokenProvider(NewStaticTokenProvider("token")),
		WithBaseURL("https://transport.test/graphql"),
		WithHTTPClient(client),
		WithRetryPolicy(fastPolicy()),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(context.Background(), Operation{Query: "{ retry }"})
	var apiErr *Error
	if !errors.As(err, &apiErr) || len(apiErr.Receipts) != 1 {
		t.Fatalf("error = %v, receipts = %#v", err, apiErr)
	}
	if got := string(apiErr.Receipts[0].Raw()); got != "retryable response" {
		t.Fatalf("retained raw response = %q", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
