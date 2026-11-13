package incident

import (
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

var errDeepText = errors.New("incident bundle text contains terminal control characters")

// validateDeepText rejects control and format characters in every string of a
// deep bundle. A capture is an untrusted text boundary: it is shared in tickets
// and replayed on another person's terminal, and deep replay prints names,
// reasons and labels directly.
//
// Caveats are the exception. They are free text, replay prints them quoted, and
// existing captures may carry any characters there.
//
// This checks text only. The restricted, Node and volume formats also bound
// sizes and counts with validateScalars; deep bundles keep the entity limits
// they already have so that existing captures stay readable.
func validateDeepText(bundle api.IncidentBundle) error {
	bundle.Caveats = nil
	return rejectControlText(reflect.ValueOf(bundle))
}

func rejectControlText(v reflect.Value) error {
	if v.Type() == reflect.TypeFor[time.Time]() {
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			return rejectControlText(v.Elem())
		}
	case reflect.Struct:
		for index := range v.NumField() {
			if err := rejectControlText(v.Field(index)); err != nil {
				return err
			}
		}
	case reflect.Slice:
		for index := range v.Len() {
			if err := rejectControlText(v.Index(index)); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			if err := rejectControlText(key); err != nil {
				return err
			}
			if err := rejectControlText(v.MapIndex(key)); err != nil {
				return err
			}
		}
	case reflect.String:
		if strings.ContainsFunc(v.String(), isTerminalControl) {
			return errDeepText
		}
	}
	return nil
}

// isTerminalControl covers C0 and C1 controls, which carry escape sequences,
// and format characters such as bidirectional overrides, which can disguise
// what a name says.
func isTerminalControl(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}
