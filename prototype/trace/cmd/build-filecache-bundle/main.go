// build-filecache-bundle creates local review artefacts. It cannot approve,
// load, upload, publish or install a programme.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

var errBuild = errors.New("candidate bundle build failed")

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, errBuild)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errBuild
	}
	flags := flag.NewFlagSet("candidate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	key := flags.String("key", "", "private signing key outside the bundle")
	output := flags.String("output", "", "new output directory")
	build := flags.String("build", "", "reproduced programme build directory")
	patch := flags.String("sdk-patch", "", "SDK policy patch bound into candidate identity")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *key == "" {
		return errBuild
	}
	switch args[0] {
	case "keygen":
		if *output != "" || *build != "" || *patch != "" {
			return errBuild
		}
		_, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return errBuild
		}
		der, err := x509.MarshalPKCS8PrivateKey(private)
		if err != nil {
			return errBuild
		}
		return writeNew(*key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	case "build":
		if *output == "" || *build == "" || *patch == "" {
			return errBuild
		}
		private, err := readKey(*key)
		if err != nil {
			return err
		}
		return buildBundle(*build, *output, *patch, private)
	default:
		return errBuild
	}
}

func readKey(path string) (ed25519.PrivateKey, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errBuild
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 || stat.Size() <= 0 || stat.Size() > 4096 {
		return nil, errBuild
	}
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(data) > 4096 {
		return nil, errBuild
	}
	block, rest := pem.Decode(data)
	if block == nil || len(rest) != 0 || block.Type != "PRIVATE KEY" {
		return nil, errBuild
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	private, ok := parsed.(ed25519.PrivateKey)
	if err != nil || !ok {
		return nil, errBuild
	}
	return private, nil
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errBuild
	}
	n, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || n != len(data) || closeErr != nil {
		return errBuild
	}
	return nil
}

func readBounded(path string, maximum int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errBuild
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > maximum {
		return nil, errBuild
	}
	data, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, errBuild
	}
	return data, nil
}

func newDirectory(path string) error {
	if !filepath.IsAbs(path) || os.Mkdir(path, 0700) != nil {
		return errBuild
	}
	return nil
}
