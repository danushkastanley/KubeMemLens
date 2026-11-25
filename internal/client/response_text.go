package client

import (
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

// neutraliseText replaces every terminal control, format and separator
// character in a decoded response with U+FFFD.
//
// A response is untrusted text: names and labels in it come from tenant
// objects and from whatever answered the request, and the CLI and TUI print
// them on an operator's terminal and write them to CSV. Replacing a character
// keeps the row visible and lets a capture of it be written; refusing the
// response would let one object hide everything else.
func neutraliseText(out any) {
	neutraliseValue(reflect.ValueOf(out))
}

func neutraliseValue(v reflect.Value) {
	switch v.Type() {
	case reflect.TypeFor[time.Time](), reflect.TypeFor[[]byte]():
		return
	case reflect.TypeFor[api.Metrics]():
		// Prometheus exposition text is line-oriented and is passed on as it is.
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			neutraliseValue(v.Elem())
		}
	case reflect.Struct:
		for index := range v.NumField() {
			if field := v.Field(index); field.CanSet() {
				neutraliseValue(field)
			}
		}
	case reflect.Slice, reflect.Array:
		for index := range v.Len() {
			neutraliseValue(v.Index(index))
		}
	case reflect.Map:
		neutraliseMap(v)
	case reflect.String:
		if text := v.String(); strings.ContainsFunc(text, isTerminalControl) {
			v.SetString(strings.Map(visibleRune, text))
		}
	}
}

// neutraliseMap rewrites entries through settable copies, because map keys and
// values cannot be changed in place.
func neutraliseMap(v reflect.Value) {
	for _, key := range v.MapKeys() {
		cleanKey := reflect.New(key.Type()).Elem()
		cleanKey.Set(key)
		neutraliseValue(cleanKey)
		cleanValue := reflect.New(v.Type().Elem()).Elem()
		cleanValue.Set(v.MapIndex(key))
		neutraliseValue(cleanValue)
		v.SetMapIndex(key, reflect.Value{})
		v.SetMapIndex(cleanKey, cleanValue)
	}
}

func visibleRune(r rune) rune {
	if isTerminalControl(r) {
		return unicode.ReplacementChar
	}
	return r
}

// isTerminalControl covers C0 and C1 controls, which carry escape sequences,
// and format and separator characters, which can disguise what a name says.
func isTerminalControl(r rune) bool {
	return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
}
