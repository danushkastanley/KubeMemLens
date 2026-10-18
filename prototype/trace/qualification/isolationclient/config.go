package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	jsonv2 "encoding/json/v2"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"k8s.io/apimachinery/pkg/util/validation"
)

var errQualification = errors.New("local isolation qualification failed")

const fixtureLabel = "kube-memlens.io/trace-isolation"

type configuration struct {
	EKS                *eksEndpointBinding `json:"eks,omitempty"`
	SchemaVersion      int                 `json:"schemaVersion"`
	Kubeconfig         string              `json:"kubeconfig"`
	Context            string              `json:"context"`
	RunID              string              `json:"runID"`
	Namespace          string              `json:"namespace"`
	Pod                string              `json:"pod"`
	Container          string              `json:"container"`
	PodUID             string              `json:"podUID"`
	ContainerID        string              `json:"containerID"`
	ContainerStartedAt time.Time           `json:"containerStartedAt"`
	NodeName           string              `json:"nodeName"`
	Kind               trace.Kind          `json:"kind"`
	ConfirmedPaths     bool                `json:"confirmedPaths"`
	DurationSeconds    int                 `json:"durationSeconds"`
	MaxEvents          uint64              `json:"maxEvents"`
}

func (configuration) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private isolation configuration]")
}
func (configuration) MarshalJSON() ([]byte, error) { return nil, errQualification }

func readConfiguration(path string) (configuration, error) {
	var c configuration
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return c, errQualification
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return c, errQualification
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 || jsonv2.Unmarshal(data, &c, jsonv2.RejectUnknownMembers(true)) != nil || c.validate() != nil {
		return configuration{}, errQualification
	}
	return c, nil
}

func (c configuration) validate() error {
	if c.SchemaVersion != 1 || c.Kubeconfig == "" || !c.validContext() ||
		len(c.RunID) != 32 || strings.Trim(c.RunID, "0123456789abcdef") != "" ||
		!strings.HasPrefix(c.Namespace, "kml-isolation-") || len(validation.IsDNS1123Label(c.Namespace)) != 0 ||
		len(validation.IsDNS1123Label(c.Pod)) != 0 || len(validation.IsDNS1123Label(c.Container)) != 0 ||
		c.PodUID == "" || len(c.PodUID) > 128 || len(c.ContainerID) != 64 || strings.Trim(c.ContainerID, "0123456789abcdef") != "" ||
		c.ContainerStartedAt.IsZero() || len(validation.IsDNS1123Subdomain(c.NodeName)) != 0 ||
		c.DurationSeconds < 1 || c.DurationSeconds > 30 || c.MaxEvents < 1 || c.MaxEvents > 10000 {
		return errQualification
	}
	bounds := trace.DefaultBounds()
	bounds.Duration = time.Duration(c.DurationSeconds) * time.Second
	bounds.Events = c.MaxEvents
	bounds.PathBytes = 64
	paths := trace.OmitPaths
	if c.ConfirmedPaths {
		paths = trace.ConfirmedPaths
	}
	if trace.ValidateIntent(c.Kind, paths, bounds) != nil {
		return errQualification
	}
	return nil
}
