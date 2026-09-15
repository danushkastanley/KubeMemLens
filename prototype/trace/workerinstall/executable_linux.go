package workerinstall

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const MaxExecutableBytes = 128 << 20
const executableSeals = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_EXEC | unix.F_SEAL_SEAL

// Executable copies an independently accepted binary to a sealed executable
// memfd. Executing this retained descriptor cannot race a path replacement or
// in-place write to the installation file. The caller owns the returned file.
func (p *Policy) Executable(path, architecture string) (*os.File, error) {
	expected, err := p.WorkerSHA256(architecture)
	if err != nil {
		return nil, err
	}
	source, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrInstallation
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() <= 0 || info.Size() > MaxExecutableBytes {
		return nil, ErrInstallation
	}
	fd, err := unix.MemfdCreate("trace-worker", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING|unix.MFD_EXEC)
	if err != nil {
		return nil, ErrInstallation
	}
	image := os.NewFile(uintptr(fd), "trace-worker")
	retained := false
	defer func() {
		if !retained {
			_ = image.Close()
		}
	}()
	hash := sha256.New()
	n, err := io.CopyBuffer(io.MultiWriter(image, hash), io.LimitReader(source, MaxExecutableBytes+1), make([]byte, 64<<10))
	if err != nil || n != info.Size() || n > MaxExecutableBytes || hex.EncodeToString(hash.Sum(nil)) != expected {
		return nil, ErrInstallation
	}
	if image.Chmod(0500) != nil {
		return nil, ErrInstallation
	}
	if _, err := unix.FcntlInt(image.Fd(), unix.F_ADD_SEALS, executableSeals); err != nil {
		return nil, ErrInstallation
	}
	seals, err := unix.FcntlInt(image.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&executableSeals != executableSeals || validateExecutable(image, architecture) != nil {
		return nil, ErrInstallation
	}
	if _, err := image.Seek(0, io.SeekStart); err != nil {
		return nil, ErrInstallation
	}
	retained = true
	return image, nil
}

func validateExecutable(image *os.File, architecture string) error {
	file, err := elf.NewFile(image)
	if err != nil {
		return ErrInstallation
	}
	defer file.Close()
	machine := elf.EM_X86_64
	if architecture == "arm64" {
		machine = elf.EM_AARCH64
	}
	if file.Class != elf.ELFCLASS64 || file.Data != elf.ELFDATA2LSB || file.Machine != machine || (file.Type != elf.ET_EXEC && file.Type != elf.ET_DYN) {
		return ErrInstallation
	}
	for _, programme := range file.Progs {
		if programme.Type == elf.PT_INTERP {
			return ErrInstallation
		}
	}
	libraries, err := file.ImportedLibraries()
	if err != nil || len(libraries) != 0 {
		return ErrInstallation
	}
	return nil
}
