package traceframe

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// DecodeText validates canonical visible escaping and the original byte limit.
// It returns sensitive original text only for explicit authorised processing;
// a terminal renderer must display the encoded representation, not this value.
func DecodeText(value string, maxBytes uint64) (string, error) {
	if maxBytes == 0 || maxBytes > 512 || len(value) > 4096 || !utf8.ValidString(value) {
		return "", ErrInvalid
	}
	var original strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '\\' {
			r, size := utf8.DecodeRuneInString(value[i:])
			original.WriteRune(r)
			i += size
		} else if strings.HasPrefix(value[i:], "\\\\") {
			original.WriteByte('\\')
			i += 2
		} else {
			if !strings.HasPrefix(value[i:], "\\u{") {
				return "", ErrInvalid
			}
			end := strings.IndexByte(value[i+3:], '}')
			if end < 4 || end > 6 {
				return "", ErrInvalid
			}
			code, err := strconv.ParseUint(value[i+3:i+3+end], 16, 32)
			if err != nil || code > utf8.MaxRune {
				return "", ErrInvalid
			}
			if !utf8.ValidRune(rune(code)) {
				return "", ErrInvalid
			}
			original.WriteRune(rune(code))
			i += 4 + end
		}
		if uint64(original.Len()) > maxBytes {
			return "", ErrInvalid
		}
	}
	raw := original.String()
	canonical, err := SafeText(raw, maxBytes)
	if err != nil || canonical != value {
		return "", ErrInvalid
	}
	return raw, nil
}
