package qualification

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The local trace image is built from the repository root with -mod=readonly,
// and its build stage copies the root module one package directory at a time.
// A root-module package that the image's commands import but the Dockerfile
// does not copy stops "docker build" with "cannot find module providing
// package". No other check builds this Dockerfile, so this test compares the
// two lists.
func TestDockerfileCopiesEveryRootModulePackageItBuilds(t *testing.T) {
	const dockerfile = "extensions/trace/Dockerfile"
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, dockerfile))
	if err != nil {
		t.Fatal(err)
	}
	copied, targets := buildStage(string(data))
	if len(copied) == 0 || len(targets) == 0 {
		t.Fatalf("%s: found %d COPY destinations and %d go build or go run targets in the build stage; this test no longer reads it correctly", dockerfile, len(copied), len(targets))
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		for _, directory := range rootModuleDirectories(t, root, architecture, targets) {
			if !slices.ContainsFunc(copied, func(destination string) bool {
				return directory == destination || strings.HasPrefix(directory, destination+"/")
			}) {
				t.Errorf("%s does not copy %s, which %s import on linux/%s: the image build fails with \"cannot find module providing package\". Add \"COPY %s ./%s\" to the build stage.",
					dockerfile, directory, strings.Join(targets, " and "), architecture, directory, directory)
			}
		}
	}
}

// buildStage returns, for the first stage of a Dockerfile, where each
// single-source COPY lands under the working directory and the package
// arguments of each "go build" and "go run".
func buildStage(dockerfile string) (copied, targets []string) {
	stages := 0
	for _, line := range strings.Split(dockerfile, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "FROM":
			stages++
		case "COPY":
			if stages == 1 && len(fields) == 3 {
				copied = append(copied, filepath.ToSlash(filepath.Clean(fields[2])))
			}
		case "RUN":
			if stages == 1 && (strings.Contains(line, " go build ") || strings.Contains(line, " go run ")) {
				for _, field := range fields {
					if strings.HasPrefix(field, "./") {
						targets = append(targets, field)
					}
				}
			}
		}
	}
	return copied, targets
}

// rootModuleDirectories lists, relative to the repository root, the
// directories of every package the targets depend on that belongs to the root
// module rather than to the trace module or a third party.
func rootModuleDirectories(t *testing.T, root, architecture string, targets []string) []string {
	t.Helper()
	traceModule := filepath.Join(root, "extensions", "trace")
	command := exec.Command("go", append([]string{"list", "-deps", "-f", "{{.Dir}}"}, targets...)...)
	command.Dir = traceModule
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+architecture, "GOFLAGS=-mod=readonly", "GOWORK=off")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list -deps %s for linux/%s: %v", strings.Join(targets, " "), architecture, err)
	}
	var directories []string
	for _, directory := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		relative, err := filepath.Rel(root, directory)
		if err != nil || strings.HasPrefix(relative, "..") {
			continue
		}
		if directory == traceModule || strings.HasPrefix(directory, traceModule+string(filepath.Separator)) {
			continue
		}
		directories = append(directories, filepath.ToSlash(relative))
	}
	return directories
}
