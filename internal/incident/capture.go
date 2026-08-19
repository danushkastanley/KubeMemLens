package incident

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

type ExistsError struct {
	Path string
}

func (err ExistsError) Error() string {
	return fmt.Sprintf("output file %s already exists; explicit overwrite confirmation is required", err.Path)
}

func Redact(bundle *api.IncidentBundle) {
	for index := range bundle.Pods {
		bundle.Pods[index].PodUID = ""
		bundle.Pods[index].Context.Labels = nil
		for containerIndex := range bundle.Pods[index].Containers {
			container := &bundle.Pods[index].Containers[containerIndex]
			container.PodUID = ""
			container.ContainerID = ""
			container.CgroupPath = ""
			container.Context.Labels = nil
		}
	}
	for index := range bundle.Histories {
		bundle.Histories[index].PodUID = ""
	}
}

func Write(stdout io.Writer, output string, overwrite bool, bundle api.IncidentBundle) error {
	if output == "-" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(bundle)
	}
	if !overwrite {
		if _, err := os.Stat(output); err == nil {
			return ExistsError{Path: output}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect output file %s: %w", output, err)
		}
	}
	directory := filepath.Dir(output)
	temporary, err := os.CreateTemp(directory, ".kube-memlens-incident-*")
	if err != nil {
		return fmt.Errorf("create incident file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect incident file: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(bundle); err != nil {
		temporary.Close()
		return fmt.Errorf("encode incident file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync incident file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close incident file: %w", err)
	}
	if overwrite {
		if err := os.Remove(output); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("replace incident file: %w", err)
		}
	}
	if err := os.Rename(temporaryName, output); err != nil {
		return fmt.Errorf("publish incident file: %w", err)
	}
	return nil
}
