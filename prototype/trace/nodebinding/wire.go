// Package nodebinding provides the private, mutually authenticated node lease
// transport. It is installed separately from the standard collector.
package nodebinding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

const maxBody = 4096
const maxLease = 15 * time.Second

type bindRequest struct {
	ID          string    `json:"id"`
	Expires     time.Time `json:"expires"`
	Namespace   string    `json:"namespace"`
	Pod         string    `json:"pod"`
	PodUID      string    `json:"podUID"`
	Container   string    `json:"container"`
	ContainerID string    `json:"containerID"`
	Started     time.Time `json:"started"`
	NodeUID     string    `json:"nodeUID"`
	NodeName    string    `json:"nodeName"`
	QoS         string    `json:"qos"`
}

func (bindRequest) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[private node request]") }
func (r bindRequest) workload() admission.Workload {
	return admission.Workload{Target: trace.TargetIdentity{Namespace: r.Namespace, PodName: r.Pod, PodUID: r.PodUID, ContainerName: r.Container, ContainerID: r.ContainerID, ContainerStartedAt: r.Started, NodeUID: r.NodeUID}, NodeName: r.NodeName, QoS: r.QoS}
}
func requestFor(id string, w admission.Workload, expires time.Time) bindRequest {
	t := w.Target
	return bindRequest{id, expires, t.Namespace, t.PodName, t.PodUID, t.ContainerName, t.ContainerID, t.ContainerStartedAt, t.NodeUID, w.NodeName, w.QoS}
}

type bindResponse struct {
	CgroupID uint64 `json:"cgroupID"`
	Profile  string `json:"profile"`
}

func (bindResponse) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[private node response]") }

func validID(id string) bool { return len(id) == 32 && strings.Trim(id, "0123456789abcdef") == "" }

// Private messages are flat and exact: reject duplicate/case-aliased keys as
// well as unknown fields. All paths use the same bounded decoder.
func decode(r io.Reader, out any, fields string) error {
	data, err := io.ReadAll(io.LimitReader(r, maxBody+1))
	if err != nil || len(data) > maxBody || !utf8.Valid(data) {
		return admission.ErrUnavailable
	}
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return admission.ErrUnavailable
	}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] || !strings.Contains("|"+fields+"|", "|"+key+"|") {
			return admission.ErrUnavailable
		}
		seen[key] = true
		var raw json.RawMessage
		if d.Decode(&raw) != nil || bytes.Equal(raw, []byte("null")) {
			return admission.ErrUnavailable
		}
	}
	if _, err = d.Token(); err != nil {
		return admission.ErrUnavailable
	}
	if _, err = d.Token(); err != io.EOF {
		return admission.ErrUnavailable
	}
	if json.Unmarshal(data, out) != nil {
		return admission.ErrUnavailable
	}
	return nil
}
