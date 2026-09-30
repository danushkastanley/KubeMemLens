package releasebundle

import (
	"bytes"
	"debug/buildinfo"
	"debug/elf"
)

// The owned bytes were size/hash checked before parsing. Binary metadata is an
// integrity cross-check, not independent proof of reproducible compilation.
func inspectExecutable(data []byte, architecture, binary string) error {
	machine := elf.EM_X86_64
	if architecture == "arm64" {
		machine = elf.EM_AARCH64
	} else if architecture != "amd64" {
		return ErrArchive
	}
	file, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return ErrArchive
	}
	defer file.Close()
	if file.Class != elf.ELFCLASS64 || file.Data != elf.ELFDATA2LSB || file.Machine != machine || (file.OSABI != elf.ELFOSABI_NONE && file.OSABI != elf.ELFOSABI_LINUX) || (file.Type != elf.ET_EXEC && file.Type != elf.ET_DYN) {
		return ErrArchive
	}
	for _, programme := range file.Progs {
		if programme.Type == elf.PT_INTERP || ((programme.Type == elf.PT_LOAD || programme.Type == elf.PT_GNU_STACK) && programme.Flags&elf.PF_X != 0 && programme.Flags&elf.PF_W != 0) {
			return ErrArchive
		}
	}
	libraries, err := file.ImportedLibraries()
	if err != nil || len(libraries) != 0 {
		return ErrArchive
	}
	info, err := buildinfo.Read(bytes.NewReader(data))
	if err != nil || info.GoVersion != "go1.27.1" {
		return ErrArchive
	}
	packagePath := "github.com/danushkastanley/kube-memlens/prototype/trace/cmd/memlens-trace"
	if binary == "memlens-filecache-worker" {
		packagePath = "github.com/danushkastanley/kube-memlens/prototype/trace/worker/cmd/memlens-filecache-worker"
	} else if binary != "memlens-trace" {
		return ErrArchive
	}
	if info.Path != packagePath {
		return ErrArchive
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		if _, exists := settings[setting.Key]; exists {
			return ErrArchive
		}
		settings[setting.Key] = setting.Value
	}
	for key, expected := range map[string]string{"GOOS": "linux", "GOARCH": architecture, "CGO_ENABLED": "0", "-trimpath": "true", "-compiler": "gc", "-buildmode": "exe"} {
		if settings[key] != expected {
			return ErrArchive
		}
	}
	return nil
}

// ValidateExecutable checks owned Linux executable bytes without running them.
// The architecture and command name must be one of the engine's fixed targets.
func ValidateExecutable(data []byte, architecture, command string) error {
	if (architecture != "amd64" && architecture != "arm64") || (command != "memlens-trace" && command != "memlens-filecache-worker") {
		return ErrArchive
	}
	return inspectExecutable(data, architecture, command)
}
