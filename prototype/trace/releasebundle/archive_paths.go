package releasebundle

import (
	"archive/tar"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

func canonicalPath(name string) bool {
	if name == "" || len(name) > 1024 || !utf8.ValidString(name) || strings.ContainsAny(name, "\\:") || strings.HasPrefix(name, "/") || path.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.Count(name, "/") >= 32 {
		return false
	}
	return strings.IndexFunc(name, unicode.IsControl) < 0
}
func memberName(h *tar.Header, maximum int64) (string, error) {
	name := h.Name
	if h.Typeflag == tar.TypeDir {
		name = strings.TrimSuffix(name, "/")
	}
	if !canonicalPath(name) || h.Linkname != "" || h.Size < 0 || h.Size > maximum || h.Uid != 0 || h.Gid != 0 || (h.Uname != "" && h.Uname != "root") || (h.Gname != "" && h.Gname != "root") || h.Mode < 0 || h.Mode & ^int64(0777) != 0 || h.Mode&0022 != 0 || len(h.Xattrs) != 0 {
		return "", ErrArchive
	}
	if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA && h.Typeflag != tar.TypeDir {
		return "", ErrArchive
	}
	if h.Typeflag == tar.TypeDir && h.Size != 0 {
		return "", ErrArchive
	}
	for key := range h.PAXRecords {
		switch key {
		case "path", "size", "mtime", "atime", "ctime":
		default:
			return "", ErrArchive
		}
	}
	return name, nil
}
func validParents(name string, directory bool, entries map[string]Entry, parents map[string]bool) bool {
	if !directory && parents[name] {
		return false
	}
	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		if existing, found := entries[parent]; found && existing.Type != Directory {
			return false
		}
		parents[parent] = true
	}
	return true
}
func validKeep(keep map[string]int64) bool {
	if len(keep) > 64 {
		return false
	}
	var total int64
	for name, maximum := range keep {
		if !canonicalPath(name) || maximum <= 0 || maximum > 4<<20 {
			return false
		}
		total += maximum
	}
	return total <= 32<<20
}
