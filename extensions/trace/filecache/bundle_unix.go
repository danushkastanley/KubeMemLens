//go:build linux || darwin

package filecache

import (
	"io"
	"os"
	"syscall"
)

// Load reads only fixed programme names and content digests beneath the retained
// installation directory. The verifier's independent key and allowlist are the
// sole authority; candidate-index.json and the bundled public key are ignored.
// It validates the accepted signature before following any content descriptors.
func (v *Verifier) Load(root *os.Root, id ArtifactID) (*Programme, error) {
	if v == nil || root == nil || !validArtifactID(id) {
		return nil, ErrArtifact
	}
	name := string(id.Kind) + "-" + id.Architecture
	manifest, err := readBundleFile(root, "programmes/"+name+".json", 2048)
	if err != nil {
		return nil, err
	}
	signature, err := readBundleFile(root, "programmes/"+name+".sig", 64)
	if err != nil {
		return nil, err
	}
	m, err := v.verifyManifest(id, manifest, signature)
	if err != nil {
		return nil, err
	}
	object, err := readBundleFile(root, "oci/blobs/sha256/"+m.ObjectSHA256, MaxObjectBytes)
	if err != nil {
		return nil, err
	}
	oci, err := readBundleFile(root, "oci/blobs/sha256/"+m.OCIManifestSHA256, 16384)
	if err != nil {
		return nil, err
	}
	return verifyContent(id, m, object, oci)
}

func readBundleFile(root *os.Root, name string, maximum int64) ([]byte, error) {
	// NONBLOCK prevents a substituted FIFO/device from hanging before fstat.
	// Root prevents intermediate symlink escapes; NOFOLLOW rejects final links.
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrArtifact
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum {
		return nil, ErrArtifact
	}
	data, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, ErrArtifact
	}
	return data, nil
}
