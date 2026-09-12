package nodestats

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

var errJSON = errors.New("invalid or unbounded node-stats JSON")

// Only recognised fields are retained. Skipped fields still consume the byte,
// token and nesting budgets, including large per-Pod and volume sections.
type decoder struct {
	json       *json.Decoder
	ctx        context.Context
	tokens     int
	depth      int
	pending    json.Token
	hasPending bool
}

func newDecoder(ctx context.Context, data []byte) *decoder {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return &decoder{json: d, ctx: ctx}
}

func (d *decoder) next() (json.Token, error) {
	if err := d.ctx.Err(); err != nil {
		return nil, err
	}
	if d.hasPending {
		d.hasPending = false
		return d.pending, nil
	}
	d.tokens++
	if d.tokens > nodecontext.MaxJSONTokens {
		return nil, errJSON
	}
	return d.json.Token()
}

func (d *decoder) absent() (bool, error) {
	token, err := d.next()
	if err != nil || token == nil {
		return token == nil, err
	}
	d.pending, d.hasPending = token, true
	return false, nil
}

func (d *decoder) enter(want json.Delim) error {
	token, err := d.next()
	if err != nil || token != want || d.depth >= nodecontext.MaxJSONDepth {
		return errJSON
	}
	d.depth++
	return nil
}

func (d *decoder) end(want json.Delim) error {
	token, err := d.next()
	d.depth--
	if err != nil || token != want {
		return errJSON
	}
	return nil
}

func (d *decoder) object(fields map[string]func() error) error {
	return d.objectFields(func(key string) func() error { return fields[key] })
}

// Lookup returns nil for discarded fields. The duplicate-key set contains only
// selected, bounded fields, never arbitrary untrusted object keys.
func (d *decoder) objectFields(lookup func(string) func() error) error {
	if err := d.enter('{'); err != nil {
		return err
	}
	seen := map[string]bool{}
	for d.json.More() {
		key, err := d.text(1 << 20)
		if err != nil {
			return err
		}
		read := lookup(key)
		if read == nil {
			if err := d.skip(); err != nil {
				return err
			}
			continue
		}
		if seen[key] {
			return errJSON
		}
		seen[key] = true
		if err := read(); err != nil {
			return err
		}
	}
	return d.end('}')
}

func (d *decoder) array(read func(int) error) error {
	if absent, err := d.absent(); absent || err != nil {
		return err
	}
	if err := d.enter('['); err != nil {
		return err
	}
	for index := 0; d.json.More(); index++ {
		if err := read(index); err != nil {
			return err
		}
	}
	return d.end(']')
}

func (d *decoder) skip() error {
	token, err := d.next()
	if err != nil {
		return err
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	if d.depth >= nodecontext.MaxJSONDepth || (delim != '{' && delim != '[') {
		return errJSON
	}
	d.depth++
	for d.json.More() {
		if err := d.skip(); err != nil {
			return err
		}
	}
	closing := json.Delim('}')
	if delim == '[' {
		closing = ']'
	}
	return d.end(closing)
}

func (d *decoder) text(maximum int) (string, error) {
	token, err := d.next()
	value, ok := token.(string)
	if err != nil || !ok || len(value) > maximum {
		return "", errJSON
	}
	return value, nil
}

func (d *decoder) stringInto(target *string, maximum int) error {
	value, err := d.text(maximum)
	*target = value
	return err
}

func (d *decoder) timeInto(target *time.Time) error {
	value, err := d.text(40)
	if err != nil {
		return err
	}
	*target, err = time.Parse(time.RFC3339Nano, value)
	return err
}

func (d *decoder) uintInto(target **uint64) error {
	token, err := d.next()
	if err != nil {
		return err
	}
	if token == nil {
		return nil
	}
	number, ok := token.(json.Number)
	if !ok {
		return errJSON
	}
	value, err := strconv.ParseUint(string(number), 10, 64)
	if err != nil {
		return errJSON
	}
	*target = &value
	return nil
}

func (d *decoder) finish() error {
	_, err := d.next()
	if !errors.Is(err, io.EOF) {
		return errJSON
	}
	return nil
}
