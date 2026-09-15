// Package testworker supplies process fixtures for Linux worker tests only.
// It is not imported by production commands or runtime packages.
package testworker

import (
	"context"
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

var buildOnce sync.Once
var binaryPath, directory string
var buildError error

// StaticBinary keeps the parent race-instrumented but builds the same package's
// child fixture without CGO/race. Production correctly rejects dynamic ELFs.
// Call Cleanup from TestMain after all tests; never remove a caller's executable.
func StaticBinary() (string, error) {
	buildOnce.Do(func() {
		binaryPath, buildError = os.Executable()
		if buildError != nil {
			return
		}
		file, err := elf.Open(binaryPath)
		if err != nil {
			buildError = err
			return
		}
		dynamic := false
		for _, p := range file.Progs {
			if p.Type == elf.PT_INTERP {
				dynamic = true
			}
		}
		if err := file.Close(); err != nil {
			buildError = err
			return
		}
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "-race" && setting.Value == "true" {
					dynamic = true
				}
			}
		}
		if !dynamic {
			return
		}
		// Match the executable build directory selected for the parent tests.
		// A separate /tmp may deliberately be mounted noexec in the harness.
		directory, buildError = os.MkdirTemp(os.Getenv("GOTMPDIR"), "kml-static-test-worker-")
		if buildError != nil {
			return
		}
		binaryPath = filepath.Join(directory, "fixture.test")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "test", "-c", "-race=false", "-mod=readonly", "-o", binaryPath, ".")
		for _, value := range os.Environ() {
			key, _, _ := strings.Cut(value, "=")
			if key != "CGO_ENABLED" && key != "GOFLAGS" && key != "GOPROXY" && key != "GOTOOLCHAIN" {
				cmd.Env = append(cmd.Env, value)
			}
		}
		cmd.Env = append(cmd.Env, "CGO_ENABLED=0", "GOFLAGS=", "GOPROXY=off", "GOTOOLCHAIN=local")
		cmd.WaitDelay = time.Second
		diagnostics := &Diagnostics{}
		cmd.Stderr = diagnostics
		if err := cmd.Run(); err != nil {
			buildError = fmt.Errorf("static worker fixture compilation failed: %w: %s", err, diagnostics.String())
		}
	})
	return binaryPath, buildError
}

func Cleanup() error {
	if directory == "" {
		return nil
	}
	return os.RemoveAll(directory)
}
