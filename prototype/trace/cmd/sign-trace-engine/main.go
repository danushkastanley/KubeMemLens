// sign-trace-engine binds reproduced worker files to a local engine declaration.
// It cannot install, execute, publish or create a runtime acceptance policy.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/releasebundle"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workerinstall"
)

var errSign = errors.New("engine declaration signing failed")

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, errSign)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("engine", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	keyPath := flags.String("key", "", "caller-owned private Ed25519 key outside output")
	output := flags.String("output", "", "new directory for public engine declaration")
	paths, pins := map[string]*string{}, map[string]*string{}
	for _, architecture := range []string{"amd64", "arm64"} {
		paths[architecture] = flags.String(architecture, "", "reproduced worker file")
		pins[architecture] = flags.String(architecture+"-sha256", "", "expected reproduced worker digest")
	}
	if flags.Parse(args) != nil || flags.NArg() != 0 || !filepath.IsAbs(*output) {
		return errSign
	}
	key, err := privateKey(*keyPath)
	if err != nil {
		return err
	}
	release := workerinstall.EngineRelease{SourceCommit: filecache.EngineSourceCommit, PatchSHA256: filecache.EnginePatchSHA256, Workers: map[string]string{}}
	for _, architecture := range []string{"amd64", "arm64"} {
		data, err := read(*paths[architecture], 128<<20, 0777)
		if err != nil || releasebundle.ValidateExecutable(data, architecture, "memlens-filecache-worker") != nil {
			return errSign
		}
		digest := sha256.Sum256(data)
		encoded := hex.EncodeToString(digest[:])
		if encoded != *pins[architecture] {
			return errSign
		}
		release.Workers[architecture] = encoded
	}
	raw, err := json.Marshal(release)
	if err != nil || os.Mkdir(*output, 0700) != nil {
		return errSign
	}
	for name, data := range map[string][]byte{
		"engine-release.json": raw, "engine-release.sig": ed25519.Sign(key, raw),
		"engine-public-key.bin": key.Public().(ed25519.PublicKey),
	} {
		file, err := os.OpenFile(filepath.Join(*output, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return errSign
		}
		_, err = file.Write(data)
		closed := file.Close()
		if err != nil || closed != nil {
			return errSign
		}
	}
	return nil
}

func privateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := read(path, 4096, 0600)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(raw)
	if block == nil || len(rest) != 0 || block.Type != "PRIVATE KEY" {
		return nil, errSign
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	key, ok := parsed.(ed25519.PrivateKey)
	if err != nil || !ok {
		return nil, errSign
	}
	return key, nil
}

func read(path string, maximum int64, permissions os.FileMode) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errSign
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errSign
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum || info.Mode().Perm()&^permissions != 0 {
		return nil, errSign
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(raw)) > maximum {
		return nil, errSign
	}
	return raw, nil
}
