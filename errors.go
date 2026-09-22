package bitquery

import (
	"errors"
	"fmt"
	"time"

	"github.com/tigusigalpa/bitquery-go/internal/redact"
)

// Kind classifies SDK errors for errors.Is checks.
type Kind int

// Error kinds classify failures from the Bitquery SDK.
const (
	// KindTransport covers network, response parsing and unexpected HTTP failures.
	KindTransport Kind = iota + 1
	// KindAuthentication covers rejected or missing credentials.
	KindAuthentication
	// KindAuthorization covers requests that the current token cannot perform.
	KindAuthorization
	// KindPlanEntitlement covers requests unavailable to the current plan.
	KindPlanEntitlement
	// KindRateLimited covers rate-limit and temporary shared-compute failures.
	KindRateLimited
	// KindServer covers HTTP 5xx failures returned by Bitquery.
	KindServer
	// KindGraphQL covers GraphQL errors returned in a successful HTTP response.
	KindGraphQL
	// KindSubscription covers WebSocket lifecycle failures.
	KindSubscription
	// KindConfig covers invalid SDK configuration or caller input.
	KindConfig
)

// Sentinel errors for errors.Is.
var (
	ErrTransport       = errors.New("bitquery: transport error")
	ErrAuthentication  = errors.New("bitquery: authentication failed (401)")
	ErrAuthorization   = errors.New("bitquery: authorization failed (403)")
	ErrPlanEntitlement = errors.New("bitquery: plan entitlement error (402)")
	ErrRateLimited     = errors.New("bitquery: rate limited (429)")
	ErrServer          = errors.New("bitquery: server error (5xx)")
	ErrGraphQL         = errors.New("bitquery: graphql errors in response")
	ErrSubscription    = errors.New("bitquery: subscription error")
	ErrInvalidConfig   = errors.New("bitquery: invalid configuration")
)

var kindSentinels = map[Kind]error{
	KindTransport:       ErrTransport,
	KindAuthentication:  ErrAuthentication,
	KindAuthorization:   ErrAuthorization,
	KindPlanEntitlement: ErrPlanEntitlement,
	KindRateLimited:     ErrRateLimited,
	KindServer:          ErrServer,
	KindGraphQL:         ErrGraphQL,
	KindSubscription:    ErrSubscription,
	KindConfig:          ErrInvalidConfig,
}

// Error is the typed SDK error. Message and Context are sanitized —
// tokens, client secrets and `token` URL params never leak.
type Error struct {
	Kind       Kind
	Message    string
	StatusCode int
	// RetryAfter carries the server hint for 429/shared-compute errors.
	RetryAfter time.Duration
	// Temporary marks documented transient failures (429, shared compute,
	// 502/503/504) that are safe to retry.
	Temporary bool
	// GraphQLErrors is populated for KindGraphQL.
	GraphQLErrors []GraphQLError
	// Response keeps the full response for partial-data inspection.
	Response *Response
	// Context holds sanitized diagnostic fields.
	Context map[string]any

	cause error
}

func (e *Error) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("bitquery: %s (HTTP %d)", e.Message, e.StatusCode)
	}
	return "bitquery: " + e.Message
}

func (e *Error) Unwrap() error {
	if e.cause != nil {
		return e.cause
	}
	return kindSentinels[e.Kind]
}

// Is reports whether target is the sentinel associated with e.Kind.
func (e *Error) Is(target error) bool {
	return target == kindSentinels[e.Kind]
}

// Wrap builds an *Error of the given kind around cause — the exported
// constructor for subpackages and consumers extending the taxonomy.
func Wrap(kind Kind, message string, cause error) *Error {
	return &Error{Kind: kind, Message: redact.String(message), cause: cause}
}

// newError builds an *Error with a sanitized message and context.
func newError(kind Kind, status int, message string, cause error) *Error {
	return &Error{
		Kind:       kind,
		StatusCode: status,
		Message:    redact.String(message),
		Context:    map[string]any{"status": status},
		cause:      cause,
	}
}
