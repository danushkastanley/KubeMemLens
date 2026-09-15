package traceframe

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
)

func addAggregateSeeds(f *testing.F) {
	for _, kind := range []trace.Kind{trace.Files, trace.Cache} {
		acc, err := traceaggregate.New(kind, 10)
		if err != nil {
			f.Fatal(err)
		}
		snapshot := acc.Snapshot()
		frame, err := NewSummaryVersion(Summary{SessionEndedAt: time.Unix(230, 0).UTC(), Termination: trace.Cancelled, Incomplete: true, Aggregates: &snapshot}, AggregateVersion)
		if err != nil {
			f.Fatal(err)
		}
		data, err := Encode(frame)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
}

func aggregateFixture(t *testing.T, kind trace.Kind) (Metadata, Frame, Summary) {
	t.Helper()
	s := spec(t, kind, trace.OmitPaths)
	start, end := time.Unix(200, 0).UTC(), time.Unix(230, 0).UTC()
	m := Metadata{strings.Repeat("b", 32), "sha256:" + strings.Repeat("c", 64), "sha256:" + strings.Repeat("d", 64), s, start, end}
	metadata, err := NewMetadataVersion(m, AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := traceaggregate.New(kind, 10)
	one, zero, requested, completed := uint64(1), uint64(0), uint64(10), uint64(8)
	if kind == trace.Files {
		if a.File(trace.FileActivity{Operation: trace.FileRead, RequestedBytes: &requested, CompletedBytes: &completed}) != nil {
			t.Fatal("fixture aggregate failed")
		}
	} else {
		if a.Cache(trace.CacheActivity{Operation: trace.CacheAdd, Pages: 512}) != nil {
			t.Fatal("fixture aggregate failed")
		}
	}
	aggregates := a.Snapshot()
	return m, metadata, Summary{SessionEndedAt: end, ObservationStartedAt: &start, ObservationEndedAt: &end, Termination: trace.Expired, EngineCounts: trace.Counts{Produced: &one, Sampled: &zero, Lost: &zero, Rejected: &zero}, WrittenBytesBeforeSummary: uint64(len(encoded(t, metadata))), Incomplete: true, Aggregates: &aggregates}
}

func TestAggregateVersionRoundTripWithoutEventFrames(t *testing.T) {
	for _, kind := range []trace.Kind{trace.Files, trace.Cache} {
		t.Run(string(kind), func(t *testing.T) {
			m, first, summary := aggregateFixture(t, kind)
			last, err := NewSummaryVersion(summary, AggregateVersion)
			if err != nil {
				t.Fatal(err)
			}
			stream := append(encoded(t, first), encoded(t, last)...)
			reader := NewReader(bytes.NewReader(stream))
			metadata, err := reader.Next()
			if err != nil || metadata.Version() != AggregateVersion {
				t.Fatal("v2 metadata rejected")
			}
			if metadata.MatchAdmissionVersion(m.SessionID, m.EngineDigest, m.ProgrammeDigest, m.Specification, m.Deadline, AggregateVersion) != nil {
				t.Fatal("v2 identity mismatch")
			}
			if metadata.MatchAdmission(m.SessionID, m.EngineDigest, m.ProgrammeDigest, m.Specification, m.Deadline) == nil {
				t.Fatal("legacy expectation accepted v2")
			}
			frame, err := reader.Next()
			if err != nil || frame.Type() != SummaryFrame {
				t.Fatal("aggregate summary rejected")
			}
			if _, err := reader.Next(); err != io.EOF {
				t.Fatal("stream did not terminate")
			}
			if _, events := reader.Counts(); events != 0 {
				t.Fatal("aggregates misreported as event frames")
			}
		})
	}
}

func TestAggregateVersionRejectsMisleadingTotalsAndLegacyEncoding(t *testing.T) {
	_, _, summary := aggregateFixture(t, trace.Files)
	frame, err := NewSummaryVersion(summary, AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	data := encoded(t, frame)
	for _, bad := range [][]byte{
		bytes.Replace(data, []byte(`"version":2`), []byte(`"version":1`), 1),
		bytes.Replace(data, []byte(`"observations":1`), []byte(`"observations":2`), 1),
		bytes.Replace(data, []byte(`"produced":1`), []byte(`"produced":0`), 1),
		bytes.Replace(data, []byte(`"value":10`), []byte(`"value":null`), 1),
		bytes.Replace(data, []byte(`"unreported":false`), []byte(`"unreported":true`), 1),
		bytes.Replace(data, []byte(`"incomplete":true`), []byte(`"incomplete":false`), 1),
		bytes.Replace(data, []byte(`"reads"`), []byte(`"Reads"`), 1),
	} {
		if _, err := Decode(bad); err == nil {
			t.Fatal("invalid aggregate representation accepted")
		}
	}
	if _, err := NewSummary(summary); err == nil {
		t.Fatal("legacy summary silently discarded aggregates")
	}
	summary.Termination = trace.AuthorisationLost
	if _, err := NewSummaryVersion(summary, AggregateVersion); err == nil {
		t.Fatal("aggregates disclosed after authorisation loss")
	}
}

func TestV2AuthorityLossRejectsEngineDataEvenWithoutAggregates(t *testing.T) {
	base := Summary{SessionEndedAt: time.Unix(230, 0).UTC(), Termination: trace.AuthorisationLost, Incomplete: true}
	frame, err := NewSummaryVersion(base, AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	data := encoded(t, frame)
	for _, key := range []string{"produced", "sampled", "lost", "rejected"} {
		bad := bytes.Replace(data, []byte(`"`+key+`":null`), []byte(`"`+key+`":0`), 1)
		if _, err := Decode(bad); err == nil {
			t.Fatal("authority loss disclosed engine count")
		}
	}
	start, end := time.Unix(200, 0).UTC(), time.Unix(230, 0).UTC()
	base.ObservationStartedAt, base.ObservationEndedAt = &start, &end
	if _, err := NewSummaryVersion(base, AggregateVersion); err == nil {
		t.Fatal("authority loss disclosed observation window")
	}
}

func TestV2RawEventsRequireConfirmedFilesAndVersionConsistency(t *testing.T) {
	_, metadata, _ := aggregateFixture(t, trace.Files)
	confirmed := spec(t, trace.Files, trace.ConfirmedPaths)
	path, _ := trace.NewSensitiveText("/confirmed", 256)
	event := trace.FileActivity{ObservedAt: time.Unix(201, 0).UTC(), Operation: trace.FileRead, Path: path}
	frame, err := NewFileVersion(event, confirmed, AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	reader := NewReader(bytes.NewReader(append(encoded(t, metadata), encoded(t, frame)...)))
	if _, err := reader.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Next(); err == nil {
		t.Fatal("default aggregate stream accepted raw event")
	}
	if _, err := NewFileVersion(event, spec(t, trace.Files, trace.OmitPaths), AggregateVersion); err == nil {
		t.Fatal("v2 omit policy constructed raw event")
	}
	legacy, err := NewFile(event, confirmed)
	if err != nil {
		t.Fatal(err)
	}
	reader = NewReader(bytes.NewReader(append(encoded(t, metadata), encoded(t, legacy)...)))
	if _, err := reader.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Next(); err == nil {
		t.Fatal("mixed stream versions accepted")
	}
}

func TestV2UnknownTotalKeepsItsReason(t *testing.T) {
	_, _, summary := aggregateFixture(t, trace.Files)
	summary.Aggregates.Reads.RequestedBytes = traceaggregate.Total{Unreported: true, Overflow: true}
	frame, err := NewSummaryVersion(summary, AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(encoded(t, frame)); err != nil {
		t.Fatal("explicit unknown total rejected")
	}
}
