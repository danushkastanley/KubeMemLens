package releasebundle

import "strings"

// MatchProgrammes checks image copies and the engine's retained compiler headers.
// It does not substitute a signed build record for actual build/reproduction logs.
func MatchProgrammes(image Image, engine Engine, programmes Programmes) error {
	if !validDigest(programmes.digest) || !validDigest(engine.digest) || len(programmes.headers) == 0 {
		return ErrArchive
	}
	for name, digest := range programmes.headers {
		entry, found := engine.files.Entry(name)
		if !found || entry.Type != Regular || entry.SHA256 != digest {
			return ErrArchive
		}
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		files, found := image.Platform(architecture)
		if !found {
			return ErrArchive
		}
		expected := make(map[string]bool)
		for _, name := range programmes.files.Names() {
			entry, _ := programmes.files.Entry(name)
			if entry.Type != Regular {
				continue
			}
			target := programmeImagePath(name)
			expected[target] = true
			actual, found := files.Entry(target)
			if !found || actual.Type != Regular || actual.SHA256 != entry.SHA256 || actual.Size != entry.Size || actual.Mode != entry.Mode {
				return ErrArchive
			}
		}
		for _, name := range files.Names() {
			entry, _ := files.Entry(name)
			if entry.Type == Regular && strings.HasPrefix(name, "opt/memlens-trace/programmes/") && !expected[name] {
				return ErrArchive
			}
		}
	}
	return nil
}
