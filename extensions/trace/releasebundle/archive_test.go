package releasebundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type member struct {
	header tar.Header
	body   string
}

func file(name, body string) member {
	return member{tar.Header{Name: name, Mode: 0600, Size: int64(len(body)), Typeflag: tar.TypeReg}, body}
}
func archive(t testing.TB, members ...member) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	for _, m := range members {
		if err := writer.WriteHeader(&m.header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(m.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
func compressed(t testing.TB, raw []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := gzip.NewWriter(&output)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
func testLimits() Limits { return Limits{1 << 20, 1 << 20, 1 << 18, 100} }
func rejects(t *testing.T, raw []byte, format Compression, limits Limits, keep map[string]int64) {
	t.Helper()
	if _, err := ReadArchive(bytes.NewReader(raw), format, limits, keep); !errors.Is(err, ErrArchive) {
		t.Fatalf("unsafe archive accepted: %v", err)
	}
}

func TestArchiveHashesFilesWithoutExtraction(t *testing.T) {
	data := archive(t, file("metadata.json", `{"version":1}`), file("payload/binary", "not executed"), file("empty", ""))
	for _, format := range []Compression{Tar, TarGzip} {
		body := data
		if format == TarGzip {
			body = compressed(t, data)
		}
		inventory, err := ReadArchive(bytes.NewReader(body), format, testLimits(), map[string]int64{"metadata.json": 64, "empty": 1})
		if err != nil || len(inventory.Names()) != 3 {
			t.Fatalf("valid archive rejected: %v", err)
		}
		inputSHA, expandedSHA := sha256.Sum256(body), sha256.Sum256(data)
		if inventory.InputDigest() != hex.EncodeToString(inputSHA[:]) || inventory.ExpandedDigest() != hex.EncodeToString(expandedSHA[:]) || inventory.InputBytes() != int64(len(body)) || inventory.ExpandedBytes() != int64(len(data)) {
			t.Fatal("compressed and expanded identities were not preserved")
		}
		entry, ok := inventory.Entry("payload/binary")
		digest := sha256.Sum256([]byte("not executed"))
		if !ok || entry.Type != Regular || entry.Size != 12 || entry.SHA256 != hex.EncodeToString(digest[:]) {
			t.Fatal("payload identity changed")
		}
		if _, err := entry.Bytes(); !errors.Is(err, ErrArchive) {
			t.Fatal("unrequested payload retained")
		}
		entry, _ = inventory.Entry("metadata.json")
		retained, _ := entry.Bytes()
		retained[0] = 'x'
		unchanged, _ := entry.Bytes()
		if string(unchanged) != `{"version":1}` {
			t.Fatal("retained metadata is mutable")
		}
		empty, _ := inventory.Entry("empty")
		if body, err := empty.Bytes(); err != nil || len(body) != 0 {
			t.Fatal("empty retained file confused with absent content")
		}
		if strings.Contains(fmt.Sprintf("%+v %+v", inventory, entry), "metadata.json") {
			t.Fatal("implicit formatting exposed archive paths")
		}
	}
}
func TestArchiveRejectsUnsafePathsAndTypes(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "./alias", "a//b", "a/../b", "a\\b", "C:drive", "line\nname", strings.Repeat("x/", 32) + "file"} {
		t.Run(name, func(t *testing.T) { rejects(t, archive(t, file(name, "x")), Tar, testLimits(), nil) })
	}
	for _, kind := range []byte{tar.TypeSymlink, tar.TypeLink, tar.TypeChar, tar.TypeBlock, tar.TypeFifo} {
		m := member{header: tar.Header{Name: "unsafe", Mode: 0600, Typeflag: kind}}
		if kind == tar.TypeSymlink || kind == tar.TypeLink {
			m.header.Linkname = "outside"
		}
		rejects(t, archive(t, m), Tar, testLimits(), nil)
	}
	for _, mode := range []int64{06755, 0777, 0664, 010644} {
		m := file("unsafe", "x")
		m.header.Mode = mode
		rejects(t, archive(t, m), Tar, testLimits(), nil)
	}
	m := file("unsafe", "x")
	m.header.Uid = 1000
	rejects(t, archive(t, m), Tar, testLimits(), nil)
	m = file("unsafe", "x")
	m.header.Uname = "private-user"
	rejects(t, archive(t, m), Tar, testLimits(), nil)
	m = file("unsafe", "x")
	m.header.PAXRecords = map[string]string{"SCHILY.xattr.security.capability": "unsafe"}
	rejects(t, archive(t, m), Tar, testLimits(), nil)
}
func TestArchiveRejectsConflictingNamesAndTrees(t *testing.T) {
	for _, members := range [][]member{
		{file("same", "x"), file("same", "x")},
		{file("parent", "x"), file("parent/child", "y")},
		{file("parent/child", "y"), file("parent", "x")},
	} {
		rejects(t, archive(t, members...), Tar, testLimits(), nil)
	}
	directory := member{header: tar.Header{Name: "parent/", Typeflag: tar.TypeDir, Mode: 0755}}
	if _, err := ReadArchive(bytes.NewReader(archive(t, directory, file("parent/child", "y"))), Tar, testLimits(), nil); err != nil {
		t.Fatal(err)
	}
}
func TestArchiveEnforcesAllResourceBounds(t *testing.T) {
	raw := archive(t, file("data", strings.Repeat("x", 4096)))
	limits := testLimits()
	limits.InputBytes = int64(len(raw) - 1)
	rejects(t, raw, Tar, limits, nil)
	limits = testLimits()
	limits.ExpandedBytes = 4096
	rejects(t, compressed(t, raw), TarGzip, limits, nil)
	limits = testLimits()
	limits.MemberBytes = 4095
	rejects(t, raw, Tar, limits, nil)
	limits = testLimits()
	limits.Members = 1
	rejects(t, archive(t, file("a", "x"), file("b", "x")), Tar, limits, nil)
	rejects(t, raw, Tar, testLimits(), map[string]int64{"data": 4095})
	rejects(t, raw, Tar, testLimits(), map[string]int64{"absent": 10})
	rejects(t, raw, Tar, testLimits(), map[string]int64{"../escape": 10})
	rejects(t, raw, Compression(9), testLimits(), nil)
	rejects(t, raw, Tar, Limits{}, nil)
}
func TestArchiveRejectsTruncationTrailingDataAndConcatenatedGzip(t *testing.T) {
	raw := archive(t, file("data", "content"))
	packed := compressed(t, raw)
	rejects(t, raw[:515], Tar, testLimits(), nil)
	rejects(t, append(append([]byte(nil), raw...), []byte("hidden")...), Tar, testLimits(), nil)
	rejects(t, append(append([]byte(nil), packed...), packed...), TarGzip, testLimits(), nil)
	rejects(t, packed[:len(packed)-1], TarGzip, testLimits(), nil)
	badCRC := append([]byte(nil), packed...)
	badCRC[len(badCRC)-5] ^= 1
	rejects(t, badCRC, TarGzip, testLimits(), nil)
	rejects(t, archive(t), Tar, testLimits(), nil)
}
func FuzzArchive(f *testing.F) {
	valid := archive(f, file("metadata.json", `{"version":1}`))
	f.Add(valid, false)
	f.Add(compressed(f, valid), true)
	f.Add([]byte("bad archive"), false)
	f.Fuzz(func(t *testing.T, data []byte, gzip bool) {
		if len(data) > 1<<20 {
			return
		}
		format := Tar
		if gzip {
			format = TarGzip
		}
		inventory, err := ReadArchive(bytes.NewReader(data), format, testLimits(), map[string]int64{"metadata.json": 4096})
		if err != nil {
			return
		}
		if len(inventory.Names()) == 0 || len(inventory.Names()) > 100 {
			t.Fatal("unbounded inventory")
		}
		for _, name := range inventory.Names() {
			if !canonicalPath(name) {
				t.Fatal("unsafe accepted path")
			}
		}
	})
}
