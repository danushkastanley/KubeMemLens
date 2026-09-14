package traceframe

import "testing"

func TestDecodeTextRejectsNonUnicodeEscapes(t *testing.T) {
	for _, value := range []string{`\u{110000}`, `\u{ffffff}`, `\u{d800}`, `\u{dfff}`, `\u{ffffffff}`} {
		if _, err := DecodeText(value, 512); err == nil {
			t.Fatal("accepted an escape outside Unicode scalar values")
		}
	}
}

func TestDecodeTextPreservesUnicodeBoundaries(t *testing.T) {
	for _, original := range []string{"\x00", "\u200e", "\U000e0001", "\U0010ffff"} {
		visible, err := SafeText(original, 512)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeText(visible, 512)
		if err != nil || decoded != original {
			t.Fatal("valid Unicode failed canonical round trip")
		}
	}
}
