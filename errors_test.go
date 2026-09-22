package bitquery

import (
	"errors"
	"strings"
	"testing"
)

func TestErrorKindSentinels(t *testing.T) {
	cases := map[Kind]error{
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
	for kind, sentinel := range cases {
		err := &Error{Kind: kind, Message: "x"}
		if !errors.Is(err, sentinel) {
			t.Fatalf("kind %v does not match its sentinel", kind)
		}
	}
}

func TestWrapSanitizesMessage(t *testing.T) {
	err := Wrap(KindSubscription, "dial wss://x.test/gql?token=SECRET123 failed", nil)
	if strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("token leaked: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("expected redaction marker: %s", err.Error())
	}
	if !errors.Is(err, ErrSubscription) {
		t.Fatal("sentinel mismatch")
	}
}

func TestErrorMessageRedacted(t *testing.T) {
	err := newError(KindTransport, 500, "call https://x?token=ABC failed", nil)
	if strings.Contains(err.Error(), "ABC") {
		t.Fatalf("token leaked: %s", err.Error())
	}
}
