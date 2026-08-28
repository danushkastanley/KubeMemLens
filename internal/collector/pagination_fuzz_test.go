package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"reflect"
	"strconv"
	"testing"
)

const maxFuzzPaginationBytes = 2 << 10

func FuzzPaginateKeys(f *testing.F) {
	for _, seed := range []struct {
		data  []byte
		limit uint16
	}{
		{data: nil, limit: 0},
		{data: []byte("one"), limit: 0},
		{data: []byte("one\x00two\x00three"), limit: 1},
		{data: []byte("repeated-repeated-repeated"), limit: 7},
		{data: []byte{0, 1, 2, 0xff, 0xfe, 0xfd}, limit: 31},
	} {
		f.Add(seed.data, seed.limit)
	}

	f.Fuzz(func(t *testing.T, data []byte, rawLimit uint16) {
		if len(data) > maxFuzzPaginationBytes {
			return
		}

		keys := paginationFuzzKeys(data)
		limit := int(rawLimit%32) + 1
		scope := "fuzz:pagination"
		query := url.Values{"limit": {strconv.Itoa(limit)}}
		seen := make(map[int]struct{}, len(keys))
		previousDigest := ""
		previousToken := ""

		for pageNumber := 0; pageNumber <= len(keys); pageNumber++ {
			page, err := PaginateKeys(keys, query, scope)
			if err != nil {
				t.Fatalf("page %d: PaginateKeys returned error: %v", pageNumber, err)
			}
			repeated, err := PaginateKeys(keys, query, scope)
			if err != nil || !reflect.DeepEqual(repeated, page) {
				t.Fatalf("page %d is not deterministic: first=%#v repeated=%#v error=%v", pageNumber, page, repeated, err)
			}
			if len(page.Indexes) > limit {
				t.Fatalf("page %d contains %d indexes, limit is %d", pageNumber, len(page.Indexes), limit)
			}

			for _, index := range page.Indexes {
				if index < 0 || index >= len(keys) {
					t.Fatalf("page %d returned out-of-range index %d for %d keys", pageNumber, index, len(keys))
				}
				if _, duplicate := seen[index]; duplicate {
					t.Fatalf("page %d returned index %d more than once", pageNumber, index)
				}
				seen[index] = struct{}{}

				digest := paginationFuzzDigest(keys[index])
				if digest <= previousDigest {
					t.Fatalf("page %d returned digest %q after %q", pageNumber, digest, previousDigest)
				}
				previousDigest = digest
			}

			if page.Continue == "" {
				if len(seen) != len(keys) {
					t.Fatalf("pagination visited %d of %d keys", len(seen), len(keys))
				}
				return
			}
			if len(page.Indexes) == 0 || page.Continue == previousToken {
				t.Fatalf("page %d returned a non-progressing continuation token", pageNumber)
			}
			cursor, err := decodeScopedContainerCursor(page.Continue, scope)
			if err != nil || cursor != previousDigest {
				t.Fatalf("page %d continuation cursor=%q, want %q, error=%v", pageNumber, cursor, previousDigest, err)
			}
			if _, err := decodeScopedContainerCursor(page.Continue, scope+":other"); err == nil {
				t.Fatalf("page %d continuation token was accepted for another scope", pageNumber)
			}

			previousToken = page.Continue
			query.Set("continue", page.Continue)
		}

		t.Fatalf("pagination did not terminate after %d pages", len(keys)+1)
	})
}

func paginationFuzzKeys(data []byte) []string {
	const chunkBytes = 8
	keys := make([]string, 0, (len(data)+chunkBytes-1)/chunkBytes)
	for start := 0; start < len(data); start += chunkBytes {
		end := min(start+chunkBytes, len(data))
		keys = append(keys, string(data[start:end])+"\x00"+strconv.Itoa(len(keys)))
	}
	return keys
}

func paginationFuzzDigest(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:])
}
