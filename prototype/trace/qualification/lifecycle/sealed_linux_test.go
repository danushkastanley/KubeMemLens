package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPartialObserverBindsTheAcceptedSealedExecutable(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])
	fd, err := unix.MemfdCreate("trace-worker", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING|unix.MFD_EXEC)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), "accepted test executable")
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := file.Chmod(0755); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectImage(file, digest); err == nil {
		t.Fatal("mutable image accepted")
	}
	if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, immutableImageSeals); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectImage(file, strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong accepted hash ignored")
	}
	parent, err := openProcess(os.Getpid(), digest, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.close()
	image, err := retainedImage(parent, digest)
	if err != nil {
		t.Fatal(err)
	}
	defer image.file.Close()
	if wrong, err := openSealedProcess(os.Getpid(), image); err == nil {
		wrong.close()
		t.Fatal("same bytes in another executable inode accepted")
	}
	cgroup, err := os.Open("/sys/fs/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	defer cgroup.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/proc/self/fd/4")
	cmd.Args = []string{"memlens-filecache-worker"}
	cmd.Env = []string{"GOTRACEBACK=none"}
	cmd.Stdin = reader
	cmd.ExtraFiles = []*os.File{cgroup, file}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	deadline := time.Now().Add(time.Second)
	var child *process
	for time.Now().Before(deadline) {
		child, err = openSealedProcess(cmd.Process.Pid, image)
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if child == nil || err != nil {
		t.Fatal("exact sealed image/lifetime not recognised")
	}
	defer child.close()
	if _, err := file.WriteAt([]byte{0}, 0); err == nil {
		t.Fatal("accepted image could be changed after validation")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal("sealed fixture did not exit normally")
	}
}
