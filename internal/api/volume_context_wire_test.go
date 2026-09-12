package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVolumeResponseRejectsDuplicateAndOversizedFields(t *testing.T) {
	for _, body := range []string{
		`{"context":{"volumes":[` + strings.Repeat(`{},`, 64) + `{}]}}`,
		`{"context":{"schemaVersion":1,"SchemaVersion":1}}`,
		`{"metadata":{"unknown":"value"}}`,
		`{"context":{"volumes":[{"health":[{},{},{},{}]}]}}`,
	} {
		var response PodVolumeContext
		if json.Unmarshal([]byte(body), &response) == nil {
			t.Fatal("hostile response accepted")
		}
	}
}

func FuzzPodVolumeContext(f *testing.F) {
	f.Add([]byte(`{"metadata":{},"context":{"volumes":[{"health":[{"observation":{"source":"csi-node-backend"},"lastGood":{"conditions":[{"status":"StorageDegraded"}]}}]}]}}`))
	f.Add([]byte(`{"kind":"PodVolumeContext","metadata":{},"context":{"schemaVersion":1,"volumes":[]}}`))
	f.Add([]byte(`{"metadata":{"name":"a","Name":"b"}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var response PodVolumeContext
		_ = json.Unmarshal(data, &response)
	})
}
