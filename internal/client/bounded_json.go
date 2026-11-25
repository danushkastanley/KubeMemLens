package client

import (
	"encoding/json"
	"errors"
	"io"
)

// legacyMaxResponseBytes bounds a response read from a collector over direct
// HTTP or the Service proxy. It equals the collector's own default response
// limit, so a healthy collector never exceeds it.
const legacyMaxResponseBytes = 16 << 20

var errResponseTooLarge = errors.New("response exceeds byte limit")

// decodeBoundedJSON decodes exactly one JSON value from at most limit bytes.
func decodeBoundedJSON(body io.Reader, limit int64, out any) error {
	limited := &io.LimitedReader{R: body, N: limit + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(out)
	if err == nil {
		if trailing := decoder.Decode(&struct{}{}); trailing != io.EOF {
			err = errors.New("unexpected trailing JSON")
		}
	}
	if limited.N == 0 {
		return errResponseTooLarge
	}
	return err
}
