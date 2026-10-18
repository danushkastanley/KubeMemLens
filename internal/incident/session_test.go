package incident

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionExportPrivateAtomicAndBounded(t *testing.T) {
	data, err := os.ReadFile("../incidentsession/testdata/authorised-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.json")
	if err := WriteSession(io.Discard, path, false, data); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private file permission lost")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("export bytes changed")
	}
	var exists ExistsError
	if err := WriteSession(io.Discard, path, false, data); !errors.As(err, &exists) {
		t.Fatal("implicit overwrite allowed")
	}
	if err := WriteSession(io.Discard, path, true, []byte(`{"invalid":"private"}`)); err == nil {
		t.Fatal("invalid export replaced prior file")
	}
	got, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("failed export damaged file")
	}
	var stdout bytes.Buffer
	if err := WriteSession(&stdout, "-", false, data); err != nil || !bytes.Equal(stdout.Bytes(), data) {
		t.Fatal("stdout export changed")
	}
	if _, err := ReadSession(path); err != nil {
		t.Fatal(err)
	}
}
