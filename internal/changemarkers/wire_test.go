package changemarkers

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func contextFixture(t testing.TB) Context {
	t.Helper()
	s, q, m := fixture()
	r, err := Compose(s, q, s.ResolvedAt, Missing, []Marker{m}, false)
	if err != nil {
		t.Fatal(err)
	}
	h, err := memoryhistory.NewReport(s, q, s.ResolvedAt)
	if err != nil {
		t.Fatal(err)
	}
	h.Series[0].Origin = "cadvisor"
	h.Series[0].SampleClock = "prometheus-sample"
	c := Context{SchemaVersion: 1, History: h, Changes: r}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestContextRoundTripAndHistoryCompatibility(t *testing.T) {
	c := contextFixture(t)
	before, _ := json.Marshal(c.History)
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip Context
	if err := json.Unmarshal(body, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if err := roundtrip.Validate(); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(roundtrip.History)
	if !bytes.Equal(before, after) {
		t.Fatal("marker envelope altered memory history")
	}
	roundtrip.Changes.UID = "same-name-replacement"
	if roundtrip.Validate() == nil {
		t.Fatal("different selected UID joined")
	}
}

func TestWireRejectsHiddenFieldsAndOversizedEvidence(t *testing.T) {
	c := contextFixture(t)
	body, _ := json.Marshal(c)
	for _, test := range []struct {
		name string
		body []byte
	}{
		{"free-form message", bytes.Replace(body, []byte(`"kind":"restarted"`), []byte(`"message":"private","kind":"restarted"`), 1)},
		{"case duplicate", bytes.Replace(body, []byte(`"uid":"pod-uid"`), []byte(`"uid":"pod-uid","UID":"other"`), 1)},
		{"trailing document", append(append([]byte(nil), body...), []byte(`{}`)...)},
		{"too many markers", bytes.Replace(body, []byte(`"markers":[`), []byte(`"markers":[`+string(bytes.Repeat([]byte(`{},`), MaxMarkers))), 1)},
		{"too long string", bytes.Replace(body, []byte(`"tenant-a"`), append(append([]byte(`"`), bytes.Repeat([]byte("a"), 513)...), '"'), 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var value Context
			if json.Unmarshal(test.body, &value) == nil {
				t.Fatal("hostile context accepted")
			}
		})
	}
}

func FuzzContextDecoder(f *testing.F) {
	seed, err := json.Marshal(contextFixture(f))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte(`{"schemaVersion":1}`))
	f.Add([]byte(`{"changes":{"markers":[{}]}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > memoryhistory.MaxResponseBytes+1 {
			return
		}
		var c Context
		if json.Unmarshal(data, &c) == nil {
			_ = c.Validate()
		}
	})
}
