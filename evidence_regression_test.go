package bitquery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

type oneShotReadCloser struct {
	data     []byte
	readErr  error
	closeErr error
	read     bool
	closes   int
	closedAt time.Time
}

func (b *oneShotReadCloser) Read(p []byte) (int, error) {
	if b.read {
		return 0, io.EOF
	}
	b.read = true
	return copy(p, b.data), b.readErr
}

func (b *oneShotReadCloser) Close() error {
	b.closes++
	b.closedAt = time.Now()
	return b.closeErr
}

func executorForBody(t *testing.T, status int, body io.ReadCloser) *Executor {
	t.Helper()
	executor, err := NewExecutor(
		V2,
		WithTokenProvider(NewStaticTokenProvider("evidence-token")),
		WithBaseURL("https://evidence.test/graphql"),
		WithHTTPClient(&http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: body}, nil
		})}),
		WithRetryPolicy(NoRetry()),
	)
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func TestHTTPReceiptKeepsBytesReturnedWithReadError(t *testing.T) {
	readErr := errors.New("reader interrupted")
	prefix := []byte(`{"data":{"large":9007199254740993}}`)
	body := &oneShotReadCloser{data: prefix, readErr: readErr}
	op := Operation{Query: "query { large }"}

	response, err := executorForBody(t, http.StatusOK, body).Execute(context.Background(), op)
	if response != nil {
		t.Fatalf("partial JSON plus read error must not parse as a response: %#v", response)
	}
	if !errors.Is(err, readErr) {
		t.Fatalf("read error was not unwrap-visible: %v", err)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || len(apiErr.Receipts) != 1 {
		t.Fatalf("error receipt = %#v", apiErr)
	}
	receipt := apiErr.Receipts[0]
	if receipt.StatusCode != http.StatusOK || !bytes.Equal(receipt.Raw(), prefix) {
		t.Fatalf("partial body evidence = status=%d raw=%q", receipt.StatusCode, receipt.Raw())
	}
	if receipt.HTTPBody != (HTTPBodyEvidence{Applicable: true, ReadError: true}) {
		t.Fatalf("partial body lifecycle = %#v", receipt.HTTPBody)
	}
	if receipt.CapturedAt.Before(body.closedAt) {
		t.Fatalf("receipt was captured before body close: receipt=%s close=%s", receipt.CapturedAt, body.closedAt)
	}
	wantOperation, marshalErr := jsonMarshalOperation(op)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if !bytes.Equal(receipt.Operation(), wantOperation) {
		t.Fatalf("operation bytes = %q, want %q", receipt.Operation(), wantOperation)
	}
	sum := sha256.Sum256(wantOperation)
	if receipt.OperationSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("operation hash = %q", receipt.OperationSHA256)
	}
	if body.closes != 1 {
		t.Fatalf("Close calls = %d, want 1", body.closes)
	}
}

func TestHTTPReceiptKeepsEmptyBodyReadError(t *testing.T) {
	readErr := errors.New("empty-body failure")
	body := &oneShotReadCloser{readErr: readErr}
	_, err := executorForBody(t, http.StatusBadGateway, body).Execute(context.Background(), Operation{Query: "query { x }"})
	if !errors.Is(err, readErr) {
		t.Fatalf("read error was not unwrap-visible: %v", err)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || len(apiErr.Receipts) != 1 || len(apiErr.Receipts[0].Raw()) != 0 {
		t.Fatalf("empty-body receipt = %#v", apiErr)
	}
	if apiErr.Receipts[0].HTTPBody != (HTTPBodyEvidence{Applicable: true, ReadError: true}) {
		t.Fatalf("empty-body lifecycle = %#v", apiErr.Receipts[0].HTTPBody)
	}
}

func TestReadLimitedKeepsLimitControlByte(t *testing.T) {
	exact, exactEvidence, err := readLimitedEvidence(bytes.NewReader([]byte("1234")), 4)
	if err != nil || string(exact) != "1234" || exactEvidence != (HTTPBodyEvidence{Applicable: true, ReadComplete: true, Complete: true}) {
		t.Fatalf("exact limit = %q, %#v, %v", exact, exactEvidence, err)
	}
	overflow, overflowEvidence, err := readLimitedEvidence(bytes.NewReader([]byte("12345")), 4)
	if !errors.Is(err, ErrResponseTooLarge) || string(overflow) != "12345" || overflowEvidence != (HTTPBodyEvidence{Applicable: true, LimitExceeded: true}) {
		t.Fatalf("overflow evidence = %q, %#v, %v", overflow, overflowEvidence, err)
	}
}

func TestHTTPBodyCloseFailuresRemainObservable(t *testing.T) {
	t.Run("close only", func(t *testing.T) {
		closeErr := errors.New("close failed")
		raw := []byte(`{"data":{"ok":true}}`)
		body := &oneShotReadCloser{data: raw, closeErr: closeErr}
		_, err := executorForBody(t, http.StatusOK, body).Execute(context.Background(), Operation{Query: "query { ok }"})
		if !errors.Is(err, closeErr) {
			t.Fatalf("close error was not unwrap-visible: %v", err)
		}
		var apiErr *Error
		if !errors.As(err, &apiErr) || len(apiErr.Receipts) != 1 || !bytes.Equal(apiErr.Receipts[0].Raw(), raw) {
			t.Fatalf("close-only evidence = %#v", apiErr)
		}
		if apiErr.Receipts[0].HTTPBody != (HTTPBodyEvidence{Applicable: true, ReadComplete: true, CloseError: true}) {
			t.Fatalf("close-only lifecycle = %#v", apiErr.Receipts[0].HTTPBody)
		}
		if body.closes != 1 {
			t.Fatalf("Close calls = %d, want 1", body.closes)
		}
	})

	t.Run("read and close", func(t *testing.T) {
		readErr := errors.New("read failed")
		closeErr := errors.New("close failed")
		raw := []byte(`{"data":`)
		body := &oneShotReadCloser{data: raw, readErr: readErr, closeErr: closeErr}
		_, err := executorForBody(t, http.StatusOK, body).Execute(context.Background(), Operation{Query: "query { ok }"})
		if !errors.Is(err, readErr) || !errors.Is(err, closeErr) {
			t.Fatalf("read/close errors must both unwrap: %v", err)
		}
		var apiErr *Error
		if !errors.As(err, &apiErr) || len(apiErr.Receipts) != 1 || !bytes.Equal(apiErr.Receipts[0].Raw(), raw) {
			t.Fatalf("read/close evidence = %#v", apiErr)
		}
		if apiErr.Receipts[0].HTTPBody != (HTTPBodyEvidence{Applicable: true, ReadError: true, CloseError: true}) {
			t.Fatalf("read/close lifecycle = %#v", apiErr.Receipts[0].HTTPBody)
		}
		if body.closes != 1 {
			t.Fatalf("Close calls = %d, want 1", body.closes)
		}
	})

	t.Run("success", func(t *testing.T) {
		body := &oneShotReadCloser{data: []byte(`{"data":{"ok":true}}`)}
		response, err := executorForBody(t, http.StatusOK, body).Execute(context.Background(), Operation{Query: "query { ok }"})
		if err != nil || response == nil || body.closes != 1 {
			t.Fatalf("success = response=%#v err=%v closes=%d", response, err, body.closes)
		}
		if len(response.Receipts) != 1 || response.Receipts[0].HTTPBody != (HTTPBodyEvidence{Applicable: true, ReadComplete: true, Complete: true}) {
			t.Fatalf("success lifecycle = %#v", response.Receipts)
		}
	})
}

func TestHTTPReceiptSurvivesBodyTriggeredCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := &cancelOnReadBody{data: []byte(`{"data":`), cancel: cancel}
	_, err := executorForBody(t, http.StatusOK, body).Execute(ctx, Operation{Query: "query { x }"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("context cancellation was not unwrap-visible: %v", err)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || len(apiErr.Receipts) != 1 || !bytes.Equal(apiErr.Receipts[0].Raw(), body.data) {
		t.Fatalf("cancelled body evidence = %#v", apiErr)
	}
	if apiErr.Receipts[0].HTTPBody != (HTTPBodyEvidence{Applicable: true, ReadError: true}) || body.closes != 1 {
		t.Fatalf("cancelled lifecycle = %#v closes=%d", apiErr.Receipts[0].HTTPBody, body.closes)
	}
}

type cancelOnReadBody struct {
	data   []byte
	cancel context.CancelFunc
	read   bool
	closes int
}

func (b *cancelOnReadBody) Read(p []byte) (int, error) {
	if b.read {
		return 0, io.EOF
	}
	b.read = true
	b.cancel()
	return copy(p, b.data), context.Canceled
}

func (b *cancelOnReadBody) Close() error {
	b.closes++
	return nil
}

func TestHTTPReadFailureRetainsAReceiptPerRetryAttempt(t *testing.T) {
	firstErr := errors.New("first read failure")
	secondErr := errors.New("second read failure")
	bodies := []*oneShotReadCloser{
		{data: []byte("first"), readErr: firstErr},
		{data: []byte("second"), readErr: secondErr},
	}
	var attempts int
	policy := fastPolicy()
	policy.MaxAttempts = 2
	executor, err := NewExecutor(
		V2,
		WithTokenProvider(NewStaticTokenProvider("evidence-token")),
		WithBaseURL("https://evidence.test/graphql"),
		WithHTTPClient(&http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			body := bodies[attempts]
			attempts++
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: body}, nil
		})}),
		WithRetryPolicy(policy),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(context.Background(), Operation{Query: "query { retry }"})
	if !errors.Is(err, secondErr) {
		t.Fatalf("final read error = %v", err)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || len(apiErr.Receipts) != 2 || string(apiErr.Receipts[0].Raw()) != "first" || string(apiErr.Receipts[1].Raw()) != "second" {
		t.Fatalf("retry receipts = %#v", apiErr)
	}
}

func TestTokenRefreshFailureRetainsPriorHTTPReceipt(t *testing.T) {
	refreshErr := errors.New("refresh unavailable")
	provider := refreshFailureProvider{err: refreshErr}
	raw := []byte(`{"error":"unauthorized"}`)
	executor, err := NewExecutor(
		V2,
		WithTokenProvider(provider),
		WithBaseURL("https://evidence.test/graphql"),
		WithHTTPClient(&http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}, nil
		})}),
		WithRetryPolicy(fastPolicy()),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(context.Background(), Operation{Query: "query { retry }"})
	if !errors.Is(err, refreshErr) {
		t.Fatalf("refresh error was not unwrap-visible: %v", err)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || len(apiErr.Receipts) != 1 || !bytes.Equal(apiErr.Receipts[0].Raw(), raw) {
		t.Fatalf("receipt before refresh failure = %#v", apiErr)
	}
}

type refreshFailureProvider struct{ err error }

func (p refreshFailureProvider) Token(context.Context) (string, error) { return "stale", nil }
func (p refreshFailureProvider) Refresh(context.Context) (string, error) {
	return "", p.err
}

func TestGraphQLErrorKeepsExactRawAndNumberLexemes(t *testing.T) {
	wantRaw := []byte(`{ "message" : "precision", "locations" : [{"line":1,"column":2}], "path" : ["amount",9007199254740993,1.2300,1e+06], "extensions" : {"large":123456789012345678901234567890,"decimal":1.2300,"scientific":1e-9,"nested":{"number":9007199254740993}} }`)
	body := append([]byte(`{"data":{"partial":true},"errors":[`), wantRaw...)
	body = append(body, ']', '}')
	response, err := parseGraphQLResponse(http.StatusOK, make(http.Header), body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response.RawBody, body) || len(response.Errors) != 1 {
		t.Fatalf("raw response/errors = %#v", response)
	}
	graphQLError := response.Errors[0]
	if !bytes.Equal(graphQLError.Raw, wantRaw) {
		t.Fatalf("error raw = %s, want %s", graphQLError.Raw, wantRaw)
	}
	for index, want := range []string{"9007199254740993", "1.2300", "1e+06"} {
		value, ok := graphQLError.Path[index+1].(json.Number)
		if !ok || value.String() != want {
			t.Fatalf("path[%d] = %#v (%T), want json.Number(%s)", index+1, graphQLError.Path[index+1], graphQLError.Path[index+1], want)
		}
	}
	if value, ok := graphQLError.Extensions["large"].(json.Number); !ok || value.String() != "123456789012345678901234567890" {
		t.Fatalf("large extension = %#v", graphQLError.Extensions["large"])
	}
	if value, ok := graphQLError.Extensions["decimal"].(json.Number); !ok || value.String() != "1.2300" {
		t.Fatalf("decimal extension = %#v", graphQLError.Extensions["decimal"])
	}
	if value, ok := graphQLError.Extensions["scientific"].(json.Number); !ok || value.String() != "1e-9" {
		t.Fatalf("scientific extension = %#v", graphQLError.Extensions["scientific"])
	}
	if value, ok := graphQLError.Locations[0]["line"].(json.Number); !ok || value.String() != "1" {
		t.Fatalf("location line = %#v", graphQLError.Locations[0]["line"])
	}
}

func TestGraphQLErrorOrderAndStrictPartialResponse(t *testing.T) {
	rawFirst := []byte(`{"message":"first","extensions":{"n":9007199254740993}}`)
	rawSecond := []byte(`{"message":"second","path":["field",1.2300]}`)
	body := append([]byte(`{"data":{"partial":true},"errors":[`), rawFirst...)
	body = append(body, ',')
	body = append(body, rawSecond...)
	body = append(body, ']', '}')

	response, err := executorForBody(t, http.StatusOK, io.NopCloser(bytes.NewReader(body))).ExecuteStrict(context.Background(), Operation{Query: "query { partial }"})
	if response == nil || !response.HasPartialData() {
		t.Fatalf("strict partial response = %#v", response)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Kind != KindGraphQL || apiErr.Response != response || len(apiErr.GraphQLErrors) != 2 {
		t.Fatalf("strict GraphQL error = %#v", apiErr)
	}
	if !bytes.Equal(apiErr.GraphQLErrors[0].Raw, rawFirst) || !bytes.Equal(apiErr.GraphQLErrors[1].Raw, rawSecond) {
		t.Fatalf("ordered raw errors = %#v", apiErr.GraphQLErrors)
	}
	if number, ok := apiErr.GraphQLErrors[0].Extensions["n"].(json.Number); !ok || number.String() != "9007199254740993" {
		t.Fatalf("strict extension number = %#v", apiErr.GraphQLErrors[0].Extensions["n"])
	}
}

func TestGraphQLErrorHandlesEmptyAndMalformedEntries(t *testing.T) {
	for _, body := range [][]byte{
		[]byte(`{"data":{"ok":true}}`),
		[]byte(`{"data":{"ok":true},"errors":null}`),
		[]byte(`{"data":{"ok":true},"errors":[]}`),
	} {
		response, err := parseGraphQLResponse(http.StatusOK, make(http.Header), body)
		if err != nil || response.HasErrors() {
			t.Fatalf("empty errors body=%s response=%#v err=%v", body, response, err)
		}
	}
	response, err := parseGraphQLResponse(http.StatusOK, make(http.Header), []byte(`{"errors":[1]}`))
	var apiErr *Error
	if err == nil || !errors.As(err, &apiErr) || apiErr.Response != response || apiErr.Kind != KindTransport {
		t.Fatalf("malformed error entry = response=%#v err=%v", response, err)
	}
	nullEntry, err := parseGraphQLResponse(http.StatusOK, make(http.Header), []byte(`{"errors":[null]}`))
	if err != nil || len(nullEntry.Errors) != 1 || nullEntry.Errors[0].Message != "unknown GraphQL error" || string(nullEntry.Errors[0].Raw) != "null" {
		t.Fatalf("null error entry = response=%#v err=%v", nullEntry, err)
	}
}

func jsonMarshalOperation(op Operation) ([]byte, error) {
	return json.Marshal(op)
}
