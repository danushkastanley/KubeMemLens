package releasebundle

import (
	"bytes"
	"testing"
)

func FuzzImage(f *testing.F) {
	valid, identity := imageFixture(f, imageChanges{})
	f.Add(valid)
	f.Add([]byte("not an OCI archive"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 || len(data) > 1<<20 {
			return
		}
		// Recompute only the enclosing checksum so fuzzing can reach the internal
		// graph checks. The independently expected image index remains fixed.
		image, err := InspectImage(bytes.NewReader(data), int64(len(data)), digest(data), identity)
		if err != nil {
			return
		}
		if image.IndexDigest() != identity.IndexDigest {
			t.Fatal("retargeted image accepted")
		}
		for _, arch := range []string{"amd64", "arm64"} {
			files, found := image.Platform(arch)
			if !found || len(files.Names()) == 0 || len(files.Names()) > 20000 {
				t.Fatal("invalid platform inventory")
			}
			for _, name := range files.Names() {
				if !traceImagePath(name) {
					t.Fatal("unexpected image path")
				}
			}
		}
	})
}
