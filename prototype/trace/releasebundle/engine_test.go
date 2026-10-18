package releasebundle

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workerinstall"
)

func engineMessage(t *testing.T) ([]byte, []byte, []byte, []byte) {
	t.Helper()
	patch, err := os.ReadFile("../worker/sdk-policy.patch")
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32))
	release := workerinstall.EngineRelease{SourceCommit: filecache.EngineSourceCommit, PatchSHA256: filecache.EnginePatchSHA256, Workers: map[string]string{"amd64": strings.Repeat("a", 64), "arm64": strings.Repeat("b", 64)}}
	data, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	return data, key.Public().(ed25519.PublicKey), ed25519.Sign(key, data), patch
}
func TestEngineReleaseBindsSignatureWorkersSDKAndPatch(t *testing.T) {
	data, key, signature, patch := engineMessage(t)
	actual, err := engineRelease(data, key, signature, patch)
	if err != nil || len(actual.Workers) != 2 {
		t.Fatal("valid engine message refused", err)
	}
	cases := []struct{ data, key, signature, patch []byte }{
		{append(append([]byte(nil), data...), '\n'), key, signature, patch},
		{data, bytes.Repeat([]byte{1}, 32), signature, patch},
		{data, key, bytes.Repeat([]byte{1}, 64), patch},
		{data, key, signature, []byte("changed patch")},
	}
	for _, tc := range cases {
		if _, err := engineRelease(tc.data, tc.key, tc.signature, tc.patch); !errors.Is(err, ErrArchive) {
			t.Fatal("changed engine identity accepted")
		}
	}
	for _, change := range []func(*workerinstall.EngineRelease){
		func(v *workerinstall.EngineRelease) { delete(v.Workers, "arm64") },
		func(v *workerinstall.EngineRelease) { v.Workers["ppc64le"] = strings.Repeat("c", 64) },
		func(v *workerinstall.EngineRelease) { v.SourceCommit = strings.Repeat("c", 40) },
		func(v *workerinstall.EngineRelease) { v.PatchSHA256 = strings.Repeat("c", 64) },
	} {
		value := actual
		value.Workers = map[string]string{"amd64": strings.Repeat("a", 64), "arm64": strings.Repeat("b", 64)}
		change(&value)
		raw, _ := json.Marshal(value)
		private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32))
		if _, err := engineRelease(raw, key, ed25519.Sign(private, raw), patch); !errors.Is(err, ErrArchive) {
			t.Fatal("signature alone bypassed the reviewed engine contract")
		}
	}
}
func TestEngineArchiveRejectsUnrecognisedFilesAndInvalidExecutables(t *testing.T) {
	data, key, signature, patch := engineMessage(t)
	members := []member{file("verification/engine-release.json", string(data)), file("verification/engine-release.sig", string(signature)), file("verification/engine-public-key.bin", string(key)), file("sources/sdk-policy.patch", string(patch))}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, name := range []string{"memlens-trace", "memlens-filecache-worker"} {
			m := file("linux-"+arch+"/"+name, "not an executable")
			m.header.Mode = 0555
			members = append(members, m)
		}
	}
	for _, extra := range []string{"", "acceptance-policy.json", "verification/private.key", "linux-amd64/unexpected"} {
		rows := append([]member(nil), members...)
		if extra != "" {
			rows = append(rows, file(extra, "x"))
		}
		raw := archive(t, rows...)
		if _, err := InspectEngine(bytes.NewReader(raw), int64(len(raw)), sum(raw)); !errors.Is(err, ErrArchive) {
			t.Fatal("invalid engine archive accepted")
		}
	}
	for _, name := range []string{"linux-amd64/memlens-trace", "linux-arm64/memlens-filecache-worker", "sources/sdk-policy.patch", "licences/go/LICENSE", "reference/engine-index.json"} {
		if !enginePath(name) {
			t.Fatal("expected public engine path refused")
		}
	}
	if inspectExecutable([]byte("not ELF"), "amd64", "memlens-trace") == nil {
		t.Fatal("non-executable accepted")
	}
}
func TestEngineImageMatchRejectsMissingExtraAndChangedContent(t *testing.T) {
	entries := map[string]Entry{}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, binary := range []string{"memlens-trace", "memlens-filecache-worker"} {
			name := "linux-" + arch + "/" + binary
			entries[name] = Entry{Name: name, Type: Regular, Mode: 0555, Size: 5, SHA256: sum([]byte(arch + binary))}
		}
	}
	entries["sources/source.tar"] = Entry{Name: "sources/source.tar", Type: Regular, Mode: 0644, Size: 5, SHA256: strings.Repeat("c", 64)}
	engine := Engine{files: FileSet{entries: entries}, digest: "sha256:" + strings.Repeat("d", 64)}
	image := Image{platforms: map[string]FileSet{}}
	for _, arch := range []string{"amd64", "arm64"} {
		files := map[string]Entry{}
		for name, entry := range entries {
			target := engineImagePath(name, arch)
			if target != "" {
				entry.Name = target
				files[target] = entry
			}
		}
		files["opt/memlens-trace/programmes/candidate-index.json"] = Entry{Name: "opt/memlens-trace/programmes/candidate-index.json", Type: Regular, Size: 10}
		image.platforms[arch] = FileSet{entries: files}
	}
	if MatchEngine(image, engine) != nil {
		t.Fatal("matching file sets refused")
	}
	files := image.platforms["arm64"].entries
	original := files["memlens-trace"]
	changed := original
	changed.SHA256 = strings.Repeat("e", 64)
	files["memlens-trace"] = changed
	if MatchEngine(image, engine) == nil {
		t.Fatal("replaced launcher accepted")
	}
	files["memlens-trace"] = original
	files["sources/extra"] = Entry{Name: "sources/extra", Type: Regular}
	if MatchEngine(image, engine) == nil {
		t.Fatal("undeclared image file accepted")
	}
	delete(files, "sources/extra")
	delete(files, "memlens-trace")
	if MatchEngine(image, engine) == nil {
		t.Fatal("missing image file accepted")
	}
	if MatchEngine(Image{}, engine) == nil || MatchEngine(image, Engine{}) == nil {
		t.Fatal("empty verification value accepted")
	}
}
