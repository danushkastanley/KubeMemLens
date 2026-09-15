// Command licencebundle retains notices for modules imported by the optional
// binary. It runs during image construction, never in the tracing runtime.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type module struct {
	Path, Version, Dir string
	Replace            *module
}
type dependency struct{ Module *module }

func main() {
	output := flag.String("output", "", "new licence output directory")
	entrypoint := flag.String("package", "./cmd/memlens-trace", "optional binary package to inventory")
	flag.Parse()
	if err := runPackage(*output, *entrypoint); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(output string) error {
	return runPackage(output, "./cmd/memlens-trace")
}

func runPackage(output, entrypoint string) error {
	if output == "" {
		return errors.New("licence output directory required")
	}
	if entrypoint != "./cmd/memlens-trace" && entrypoint != "./cmd/memlens-filecache-worker" {
		return errors.New("unsupported optional binary package")
	}
	command := exec.Command("go", "list", "-mod=readonly", "-deps", "-json", entrypoint)
	command.Stderr = os.Stderr
	stream, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	modules := map[string]module{}
	decoder := json.NewDecoder(stream)
	for {
		var pkg dependency
		err = decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return err
		}
		if pkg.Module != nil {
			modules[pkg.Module.Path] = *pkg.Module
		}
	}
	if err := command.Wait(); err != nil {
		return err
	}
	if err := os.Mkdir(output, 0755); err != nil {
		return err
	}
	keys := make([]string, 0, len(modules))
	for key := range modules {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	inventory := map[string][]string{}
	for _, key := range keys {
		m := modules[key]
		if m.Replace != nil {
			m.Dir = m.Replace.Dir
		}
		names, err := notices(m.Dir)
		if err != nil {
			return fmt.Errorf("licence notices for %s: %w", key, err)
		}
		directory := filepath.Join(output, strings.ReplaceAll(key, "/", "_"))
		if err := os.Mkdir(directory, 0755); err != nil {
			return err
		}
		for _, name := range names {
			data, err := os.ReadFile(filepath.Join(m.Dir, name))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(directory, name), data, 0644); err != nil {
				return err
			}
		}
		inventory[key+"@"+m.Version] = names
	}
	data, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, "inventory.json"), append(data, '\n'), 0644)
}
func notices(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	var result []string
	hasLicence := false
	for _, entry := range entries {
		name := strings.ToUpper(entry.Name())
		if entry.Type().IsRegular() && (strings.HasPrefix(name, "LICENSE") || strings.HasPrefix(name, "LICENCE") || strings.HasPrefix(name, "COPYING")) {
			hasLicence = true
		}
		if entry.Type().IsRegular() && (strings.HasPrefix(name, "LICENSE") || strings.HasPrefix(name, "LICENCE") || strings.HasPrefix(name, "COPYING") || strings.HasPrefix(name, "NOTICE") || strings.HasPrefix(name, "PATENTS")) {
			result = append(result, entry.Name())
		}
	}
	if !hasLicence {
		return nil, errors.New("no module licence file found")
	}
	return result, nil
}
