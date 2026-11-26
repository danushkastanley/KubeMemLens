package workerinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

const policySeals = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_EXEC | unix.F_SEAL_SEAL

// Descriptor transfers a frozen installation policy separately from request
// input. It is sealed and non-executable. The caller owns the descriptor.
func (p *Policy) Descriptor() (*os.File, error) {
	if p == nil || len(p.encoded) == 0 {
		return nil, ErrInstallation
	}
	fd, err := unix.MemfdCreate("trace-policy", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING|unix.MFD_NOEXEC_SEAL)
	if err != nil {
		return nil, ErrInstallation
	}
	file := os.NewFile(uintptr(fd), "trace-policy")
	retained := false
	defer func() {
		if !retained {
			_ = file.Close()
		}
	}()
	if n, err := file.Write(p.encoded); err != nil || n != len(p.encoded) {
		return nil, ErrInstallation
	}
	if file.Chmod(0400) != nil {
		return nil, ErrInstallation
	}
	if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, policySeals); err != nil {
		return nil, ErrInstallation
	}
	if _, err := ReadDescriptor(file); err != nil {
		return nil, err
	}
	retained = true
	return file, nil
}

// ReadDescriptor uses positional reads. Separate workers may share a dup'ed
// open-file description without racing its seek offset or mutating the policy.
func ReadDescriptor(file *os.File) (*Policy, error) {
	if file == nil {
		return nil, ErrInstallation
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 != 0 || info.Size() <= 0 || info.Size() > MaxPolicyBytes {
		return nil, ErrInstallation
	}
	seals, err := unix.FcntlInt(file.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&policySeals != policySeals {
		return nil, ErrInstallation
	}
	data, err := io.ReadAll(io.NewSectionReader(file, 0, info.Size()))
	if err != nil {
		return nil, ErrInstallation
	}
	return Parse(data)
}

// VerifyRunning proves that the accepted inherited image is the executable of
// this process, not merely an unrelated valid descriptor supplied alongside it.
func (p *Policy) VerifyRunning(image *os.File) error {
	if p == nil || image == nil {
		return ErrInstallation
	}
	info, err := image.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxExecutableBytes {
		return ErrInstallation
	}
	self, err := os.Stat("/proc/self/exe")
	if err != nil || !os.SameFile(info, self) {
		return ErrInstallation
	}
	seals, err := unix.FcntlInt(image.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&executableSeals != executableSeals {
		return ErrInstallation
	}
	expected, err := p.WorkerSHA256(runtime.GOARCH)
	if err != nil {
		return err
	}
	hash := sha256.New()
	if _, err := io.CopyBuffer(hash, io.NewSectionReader(image, 0, info.Size()), make([]byte, 64<<10)); err != nil || hex.EncodeToString(hash.Sum(nil)) != expected {
		return ErrInstallation
	}
	return validateExecutable(image, runtime.GOARCH)
}
