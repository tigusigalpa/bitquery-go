package bitquery

import (
	"strings"
	"testing"
)

func TestReadLimitedRejectsOversizedResponse(t *testing.T) {
	_, err := readLimited(strings.NewReader("12345"), 4)
	if err == nil {
		t.Fatal("expected size-limit error")
	}
}
