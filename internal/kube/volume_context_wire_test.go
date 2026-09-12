package kube

import (
	"strings"
	"testing"
)

func TestBoundedVolumeObjectBeforeTypedAllocation(t *testing.T) {
	for _, field := range []string{"volumes", "volumeMounts", "ownerReferences"} {
		valid := `{"spec":{"` + field + `":[` + strings.Repeat(`{},`, 63) + `{}]}}`
		if err := boundedVolumeObject([]byte(valid)); err != nil {
			t.Fatal(field, err)
		}
		oversized := `{"spec":{"` + field + `":[` + strings.Repeat(`{},`, 64) + `{}]}}`
		if boundedVolumeObject([]byte(oversized)) == nil {
			t.Fatal("oversized typed array accepted", field)
		}
	}
	for _, body := range []string{
		`{"spec":{"volumes":[],"Volumes":[]}}`,
		`{"spec":{"containers":[` + strings.Repeat(`{},`, 256) + `{}]}}`,
		strings.Repeat(`{"a":`, 34) + `0` + strings.Repeat(`}`, 34),
		`{} {}`, strings.Repeat(" ", maxHealthResponse+1),
	} {
		if boundedVolumeObject([]byte(body)) == nil {
			t.Fatal("hostile upstream response accepted")
		}
	}
}

func FuzzBoundedVolumeObject(f *testing.F) {
	f.Add([]byte(`{"kind":"Pod","spec":{"volumes":[]}}`))
	f.Add([]byte(`{"spec":{"volumes":[],"Volumes":[]}}`))
	f.Fuzz(func(t *testing.T, body []byte) { _ = boundedVolumeObject(body) })
}
