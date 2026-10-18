package releasebundle

import "strings"

// MatchEngine binds image files to the separately packaged engine for each
// architecture. Programme files remain the separate MatchProgrammes boundary.
func MatchEngine(image Image, engine Engine) error {
	if !validDigest(engine.digest) {
		return ErrArchive
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		files, found := image.Platform(architecture)
		if !found {
			return ErrArchive
		}
		expected := make(map[string]Entry)
		for _, name := range engine.files.Names() {
			entry, _ := engine.files.Entry(name)
			if entry.Type != Regular {
				continue
			}
			target := engineImagePath(name, architecture)
			if target == "" {
				continue
			}
			expected[target] = entry
			actual, found := files.Entry(target)
			if !found || actual.Type != Regular || actual.SHA256 != entry.SHA256 || actual.Size != entry.Size || actual.Mode != entry.Mode {
				return ErrArchive
			}
		}
		for _, name := range files.Names() {
			entry, _ := files.Entry(name)
			if entry.Type == Regular && !strings.HasPrefix(name, "opt/memlens-trace/programmes/") {
				if _, found := expected[name]; !found {
					return ErrArchive
				}
			}
		}
	}
	return nil
}
func engineImagePath(name, architecture string) string {
	prefix := "linux-" + architecture + "/"
	if strings.HasPrefix(name, prefix) {
		switch strings.TrimPrefix(name, prefix) {
		case "memlens-trace":
			return "memlens-trace"
		case "memlens-filecache-worker":
			return "opt/memlens-trace/memlens-filecache-worker"
		default:
			return ""
		}
	}
	if strings.HasPrefix(name, "linux-") {
		return ""
	}
	if strings.HasPrefix(name, "reference/") || strings.HasPrefix(name, "verification/") {
		return "opt/memlens-trace/" + name
	}
	return name
}
