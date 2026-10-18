package tracecompat

import (
	"errors"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestContractIntersectionAndLegacyBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		offer  []string
		server Range
		want   Version
		err    error
	}{
		{"legacy server-first rollout", nil, Range{1, 1}, Legacy, nil},
		{"current", []string{"1-1"}, Range{1, 1}, 1, nil},
		{"future reader retains current", []string{"1-2"}, Range{1, 1}, 1, nil},
		{"older reader on newer server", []string{"1-1"}, Range{1, 2}, 1, nil},
		{"highest common", []string{"1-3"}, Range{2, 4}, 3, nil},
		{"reader too new", []string{"2-3"}, Range{1, 1}, Legacy, ErrIncompatible},
		{"server too new", []string{"1-1"}, Range{2, 3}, Legacy, ErrIncompatible},
		{"legacy cannot bypass raised minimum", nil, Range{2, 3}, Legacy, ErrIncompatible},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Negotiate(tc.offer, tc.server)
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("got %d/%v, want %d/%v", got, err, tc.want, tc.err)
			}
		})
	}
}
func TestAmbiguousAndUnboundedOffersFail(t *testing.T) {
	for _, offer := range [][]string{{""}, {"1"}, {"0-1"}, {"1-0"}, {"2-1"}, {"01-1"}, {"+1-1"}, {"1-1 "}, {"1-1,1-1"}, {"1-1", "1-1"}, {"1-65536"}, {"1-99999999999999999"}} {
		if _, err := Negotiate(offer, Range{1, 1}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted offer %q", offer)
		}
	}
	for _, server := range []Range{{0, 1}, {2, 1}} {
		if _, err := Negotiate([]string{"1-1"}, server); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid server range accepted")
		}
	}
}
func TestAcknowledgementCannotSilentlyDowngrade(t *testing.T) {
	if AcceptResponse([]string{"1"}) != nil {
		t.Fatal("current response rejected")
	}
	for _, value := range [][]string{nil, {""}, {"0"}, {"2"}, {"01"}, {"1,1"}, {"1", "1"}} {
		if !errors.Is(AcceptResponse(value), ErrIncompatible) {
			t.Fatal("invalid acknowledgement accepted")
		}
	}
}
func TestInstalledFormatsAreNotInterchangeable(t *testing.T) {
	for _, kind := range []trace.Kind{trace.Files, trace.Cache, trace.OOM, "unknown"} {
		for version := 0; version <= 4; version++ {
			want := (kind == trace.Files || kind == trace.Cache) && version == 2 || kind == trace.OOM && version == 3
			if StreamCompatible(kind, version) != want {
				t.Fatal("unsupported stream semantics accepted")
			}
		}
	}
}
func FuzzNegotiationNeverSelectsOutsideIntersection(f *testing.F) {
	for _, s := range []string{"1-1", "1-2", "2-3", "0-65535", "1-1,1-1", ""} {
		f.Add(s, uint16(1), uint16(1))
	}
	f.Fuzz(func(t *testing.T, offer string, low, high uint16) {
		selected, err := Negotiate([]string{offer}, Range{low, high})
		if err == nil && (selected == Legacy || uint16(selected) < low || uint16(selected) > high) {
			t.Fatal("selection escaped server range")
		}
	})
}
