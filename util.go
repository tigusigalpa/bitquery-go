package bitquery

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ErrResponseTooLarge identifies an HTTP response body that exceeded the
// query response limit. The associated receipt retains at most limit+1 bytes:
// the configured limit plus one control byte proving overflow.
var ErrResponseTooLarge = errors.New("bitquery: response exceeds configured size limit")

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, _, err := readLimitedEvidence(r, limit)
	return data, err
}

func readLimitedEvidence(r io.Reader, limit int64) ([]byte, HTTPBodyEvidence, error) {
	evidence := HTTPBodyEvidence{Applicable: true}
	if limit < 0 {
		return nil, evidence, fmt.Errorf("response size limit cannot be negative")
	}
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		evidence.ReadError = true
		return data, evidence, err
	}
	if int64(len(data)) > limit {
		evidence.LimitExceeded = true
		return data, evidence, fmt.Errorf("%w: %d-byte limit", ErrResponseTooLarge, limit)
	}
	evidence.ReadComplete = true
	evidence.Complete = true
	return data, evidence, nil
}

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
