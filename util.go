package bitquery

import (
	"encoding/json"
	"fmt"
	"io"
)

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("response size limit cannot be negative")
	}
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d-byte limit", limit)
	}
	return data, nil
}

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
