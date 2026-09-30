package traceframe

import (
	"fmt"
	"strings"
	"testing"
)

func TestVisibleTextASCIIBoundaries(t *testing.T) {
	for code := 0; code < 128; code++ {
		raw := "/before/" + string(rune(code)) + "/after"
		want := raw
		switch {
		case code == '\\':
			want = `/before/\\/after`
		case code < 32 || code == 127:
			want = fmt.Sprintf(`/before/\u{%04x}/after`, code)
		}
		visible, err := SafeText(raw, uint64(len(raw)))
		if err != nil || visible != want {
			t.Fatalf("ASCII code %d was not escaped as required", code)
		}
		decoded, err := DecodeText(want, uint64(len(raw)))
		if err != nil || decoded != raw {
			t.Fatalf("ASCII code %d failed canonical decoding", code)
		}
		if want != raw {
			if _, err := DecodeText(raw, 512); err == nil {
				t.Fatalf("ASCII code %d bypassed visible escaping", code)
			}
		}
	}
}

func TestPlainTextByteLimits(t *testing.T) {
	for _, size := range []int{0, 1, 511, 512, 513} {
		value := strings.Repeat("x", size)
		for _, limit := range []uint64{0, 1, 511, 512, 513} {
			valid := limit > 0 && limit <= 512 && uint64(size) <= limit
			for _, operation := range []func(string, uint64) (string, error){SafeText, DecodeText} {
				got, err := operation(value, limit)
				if (err == nil) != valid || (valid && got != value) {
					t.Fatalf("incorrect text limit: size=%d limit=%d", size, limit)
				}
			}
		}
	}
}

func BenchmarkPlainText(b *testing.B) {
	value := strings.Repeat("/ordinary-file", 16)
	for name, operation := range map[string]func(string, uint64) (string, error){"encode": SafeText, "decode": DecodeText} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				got, err := operation(value, 512)
				if err != nil || got != value {
					b.Fatal("plain text changed")
				}
			}
		})
	}
}
