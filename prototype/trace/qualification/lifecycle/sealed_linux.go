package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

const immutableImageSeals = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_EXEC | unix.F_SEAL_SEAL

type sealedImage struct {
	file     *os.File
	identity unix.Stat_t
}

func inspectImage(file *os.File, digest string) (*sealedImage, error) {
	if len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
		return nil, errOwnership
	}
	var identity unix.Stat_t
	seals, err := unix.FcntlInt(file.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&immutableImageSeals != immutableImageSeals || unix.Fstat(int(file.Fd()), &identity) != nil || identity.Mode&unix.S_IFMT != unix.S_IFREG || identity.Size <= 0 || identity.Size > 128<<20 {
		return nil, errOwnership
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.NewSectionReader(file, 0, identity.Size))
	if err != nil || n != identity.Size || hex.EncodeToString(hash.Sum(nil)) != digest {
		return nil, errOwnership
	}
	return &sealedImage{file, identity}, nil
}

// The descriptor name only locates a candidate. Immutable seals plus the exact
// accepted hash establish its identity before any fixture worker starts.
func retainedImage(parent *process, digest string) (result *sealedImage, resultErr error) {
	defer func() {
		if resultErr != nil && result != nil {
			_ = result.file.Close()
			result = nil
		}
	}()
	entries, err := os.ReadDir(procPath(parent.pid, "fd"))
	if err != nil || len(entries) > 256 {
		return nil, errOwnership
	}
	for _, entry := range entries {
		path := procPath(parent.pid, "fd/"+entry.Name())
		name, err := os.Readlink(path)
		if err != nil || name != "/memfd:trace-worker (deleted)" {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			return result, errOwnership
		}
		candidate, err := inspectImage(file, digest)
		if err != nil {
			_ = file.Close()
			return result, err
		}
		if result != nil {
			same := result.identity.Dev == candidate.identity.Dev && result.identity.Ino == candidate.identity.Ino
			if file.Close() != nil || !same {
				return result, errOwnership
			}
			continue
		}
		result = candidate
	}
	if result == nil || parent.check() != nil {
		return result, errOwnership
	}
	return result, nil
}

func openSealedProcess(pid int, image *sealedImage) (*process, error) {
	if pid <= 1 || pid > 1<<31-1 || image == nil {
		return nil, errOwnership
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, errOwnership
	}
	p := &process{pid: pid, fd: fd}
	accepted := false
	defer func() {
		if !accepted {
			_ = p.close()
		}
	}()
	data, err := readBounded(procPath(pid, "stat"), 4096)
	if err != nil {
		return nil, errOwnership
	}
	p.start, err = startTime(data)
	if err != nil {
		return nil, errOwnership
	}
	file, err := os.Open(procPath(pid, "exe"))
	if err != nil {
		return nil, errOwnership
	}
	var identity unix.Stat_t
	statErr := unix.Fstat(int(file.Fd()), &identity)
	closeErr := file.Close()
	if statErr != nil || closeErr != nil || identity.Dev != image.identity.Dev || identity.Ino != image.identity.Ino || identity.Size != image.identity.Size || p.check() != nil {
		return nil, errOwnership
	}
	accepted = true
	return p, nil
}
