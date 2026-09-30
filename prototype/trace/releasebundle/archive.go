// Package releasebundle inspects offline release content. It never grants
// installation acceptance, runs candidate code or loads a kernel programme.
package releasebundle

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
)

var ErrArchive = errors.New("release archive violates its content or resource contract")

type Compression uint8

const (
	Tar Compression = iota
	TarGzip
)

type Limits struct {
	InputBytes, ExpandedBytes, MemberBytes int64
	Members                                int
}

func (l Limits) valid() bool {
	return l.InputBytes > 0 && l.InputBytes <= 1<<30 && l.ExpandedBytes > 0 && l.ExpandedBytes <= 2<<30 && l.MemberBytes > 0 && l.MemberBytes <= l.ExpandedBytes && l.Members > 0 && l.Members <= 20000
}

type EntryType uint8

const (
	Regular EntryType = iota
	Directory
)

// Entry contains copied metadata. Retained content is immutable and optional.
type Entry struct {
	Name   string
	Type   EntryType
	Mode   int64
	Size   int64
	SHA256 string
	data   *string
	offset int64
}

func (Entry) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[release archive entry]") }
func (e Entry) Bytes() ([]byte, error) {
	if e.data == nil {
		return nil, ErrArchive
	}
	return []byte(*e.data), nil
}

type Inventory struct {
	files                       FileSet
	inputDigest, expandedDigest string
	inputSize, expandedSize     int64
}

func (Inventory) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[release archive inventory]") }
func (i Inventory) ExpandedBytes() int64     { return i.expandedSize }
func (i Inventory) InputBytes() int64        { return i.inputSize }
func (i Inventory) InputDigest() string      { return i.inputDigest }
func (i Inventory) ExpandedDigest() string   { return i.expandedDigest }

func (i Inventory) Entry(name string) (Entry, bool) { return i.files.Entry(name) }
func (i Inventory) Names() []string                 { return i.files.Names() }

type FileSet struct{ entries map[string]Entry }

func (FileSet) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[release file set]") }
func (i FileSet) Entry(name string) (Entry, bool) {
	e, ok := i.entries[name]
	return e, ok
}
func (i FileSet) Names() []string {
	names := make([]string, 0, len(i.entries))
	for name := range i.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ReadArchive hashes regular files without extracting them. keep names the small
// metadata files whose bytes the caller needs; at most 32 MiB may be retained.
// The caller must separately authenticate the enclosing payload digest.
func ReadArchive(source io.Reader, compression Compression, limits Limits, keep map[string]int64) (Inventory, error) {
	if source == nil || !limits.valid() || (compression != Tar && compression != TarGzip) || !validKeep(keep) {
		return Inventory{}, ErrArchive
	}
	input := &io.LimitedReader{R: source, N: limits.InputBytes + 1}
	inputHash, expandedHash := sha256.New(), sha256.New()
	buffered := bufio.NewReader(io.TeeReader(input, inputHash))
	var decoded io.Reader = buffered
	var compressed *gzip.Reader
	if compression == TarGzip {
		var err error
		compressed, err = gzip.NewReader(buffered)
		if err != nil {
			return Inventory{}, ErrArchive
		}
		defer compressed.Close()
		compressed.Multistream(false)
		decoded = compressed
	}
	expanded := &io.LimitedReader{R: io.TeeReader(decoded, expandedHash), N: limits.ExpandedBytes + 1}
	inventory, err := readMembers(tar.NewReader(expanded), limits, keep, func() int64 { return limits.ExpandedBytes + 1 - expanded.N })
	if err != nil {
		return Inventory{}, ErrArchive
	}
	// tar.Reader stops at the terminator. Refuse hidden second archives or data;
	// ordinary zero padding is permitted within the total expanded-byte bound.
	if _, err := io.Copy(zeroPadding{}, expanded); err != nil || input.N <= 0 || expanded.N <= 0 {
		return Inventory{}, ErrArchive
	}
	if compression == TarGzip {
		if _, err := buffered.ReadByte(); err != io.EOF {
			return Inventory{}, ErrArchive
		}
	}
	inventory.inputSize = limits.InputBytes + 1 - input.N
	inventory.expandedSize = limits.ExpandedBytes + 1 - expanded.N
	inventory.inputDigest = hex.EncodeToString(inputHash.Sum(nil))
	inventory.expandedDigest = hex.EncodeToString(expandedHash.Sum(nil))
	return inventory, nil
}

func readMembers(reader *tar.Reader, limits Limits, keep map[string]int64, position func() int64) (Inventory, error) {
	inventory := Inventory{files: FileSet{entries: make(map[string]Entry)}}
	parents := make(map[string]bool)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil || len(inventory.files.entries) >= limits.Members {
			return Inventory{}, ErrArchive
		}
		name, err := memberName(header, limits.MemberBytes)
		if err != nil || inventory.files.entries[name].Name != "" || !validParents(name, header.Typeflag == tar.TypeDir, inventory.files.entries, parents) {
			return Inventory{}, ErrArchive
		}
		entry := Entry{Name: name, Type: Directory, Mode: header.Mode}
		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			entry.Type, entry.Size, entry.offset = Regular, header.Size, position()
			digest := sha256.New()
			var retained bytes.Buffer
			var destination io.Writer = digest
			maximum, retain := keep[name]
			if retain {
				if header.Size > maximum {
					return Inventory{}, ErrArchive
				}
				destination = io.MultiWriter(digest, &retained)
			}
			if count, err := io.Copy(destination, reader); err != nil || count != header.Size {
				return Inventory{}, ErrArchive
			}
			entry.SHA256 = hex.EncodeToString(digest.Sum(nil))
			if retain {
				data := retained.String()
				entry.data = &data
			}
		}
		inventory.files.entries[name] = entry
	}
	if len(inventory.files.entries) == 0 {
		return Inventory{}, ErrArchive
	}
	for name := range keep {
		entry, found := inventory.files.entries[name]
		if !found || entry.Type != Regular {
			return Inventory{}, ErrArchive
		}
	}
	return inventory, nil
}

type zeroPadding struct{}

func (zeroPadding) Write(data []byte) (int, error) {
	for _, b := range data {
		if b != 0 {
			return 0, ErrArchive
		}
	}
	return len(data), nil
}
