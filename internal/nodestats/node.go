package nodestats

import (
	"context"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"github.com/danushkastanley/kube-memlens/internal/resourcemetrics"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/version"
)

const maxNodeBytes = 1 << 20

type resourceColumn uint8

const (
	capacityColumn resourceColumn = iota
	allocatableColumn
)

type nodeTarget struct {
	name    string
	uid     string
	ip      string
	port    uint64
	os      string
	version string
	context nodecontext.KubernetesContext
}

func decodeNode(ctx context.Context, data []byte, name string, now time.Time) (nodeTarget, error) {
	if len(data) > maxNodeBytes {
		return nodeTarget{}, &Error{Reason: nodecontext.ResponseTooLarge}
	}
	value := nodeTarget{context: nodecontext.KubernetesContext{CapturedAt: now, MemoryPressure: "Unknown"}}
	d := newDecoder(ctx, data)
	err := d.object(map[string]func() error{
		"metadata": func() error {
			return d.object(map[string]func() error{
				"name": func() error { return d.stringInto(&value.name, nodecontext.MaxNodeNameBytes) },
				"uid":  func() error { return d.stringInto(&value.uid, nodecontext.MaxNodeUIDBytes) },
			})
		},
		"status": func() error { return d.nodeStatus(&value) },
	})
	if err != nil || d.finish() != nil || value.name != name || !validUID(value.uid) || value.ip == "" || value.port == 0 || value.port > 65535 {
		return nodeTarget{}, &Error{Reason: nodecontext.InvalidTarget}
	}
	v, err := version.ParseSemantic(value.version)
	if err != nil || value.os != "linux" || v.Major() != 1 || v.Minor() < 36 || v.Minor() > 37 {
		return nodeTarget{}, &Error{Reason: nodecontext.Unsupported}
	}
	sort.Slice(value.context.Hugepages, func(i, j int) bool { return value.context.Hugepages[i].Resource < value.context.Hugepages[j].Resource })
	return value, nil
}

func validUID(value string) bool {
	if value == "" || len(value) > nodecontext.MaxNodeUIDBytes {
		return false
	}
	for _, ch := range value {
		if ch != '-' && ch != '_' && (ch < '0' || ch > '9') && (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') {
			return false
		}
	}
	return true
}

func (d *decoder) nodeStatus(value *nodeTarget) error {
	return d.object(map[string]func() error{
		"nodeInfo": func() error {
			return d.object(map[string]func() error{
				"operatingSystem": func() error { return d.stringInto(&value.os, 32) },
				"kubeletVersion":  func() error { return d.stringInto(&value.version, 128) },
			})
		},
		"addresses": func() error { return d.addresses(value) },
		"daemonEndpoints": func() error {
			return d.object(map[string]func() error{
				"kubeletEndpoint": func() error {
					return d.object(map[string]func() error{
						"Port": func() error {
							var port *uint64
							if err := d.uintInto(&port); err != nil || port == nil {
								return errJSON
							}
							value.port = *port
							return nil
						},
					})
				},
			})
		},
		"capacity": func() error {
			return d.resources(&value.context.CapacityBytes, &value.context.Hugepages, capacityColumn)
		},
		"allocatable": func() error {
			return d.resources(&value.context.AllocatableBytes, &value.context.Hugepages, allocatableColumn)
		},
		"conditions": func() error { return d.conditions(value) },
	})
}

func (d *decoder) addresses(value *nodeTarget) error {
	return d.array(func(index int) error {
		if index >= 64 {
			return errJSON
		}
		var kind, address string
		err := d.object(map[string]func() error{
			"type":    func() error { return d.stringInto(&kind, 64) },
			"address": func() error { return d.stringInto(&address, 253) },
		})
		if err != nil {
			return err
		}
		if kind != "InternalIP" {
			return nil
		}
		ip := net.ParseIP(address)
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			return errJSON
		}
		candidate := ip.String()
		// Stable choice for dual-stack Nodes, independent of response ordering.
		if value.ip == "" || candidate < value.ip {
			value.ip = candidate
		}
		return nil
	})
}

func (d *decoder) conditions(value *nodeTarget) error {
	seen := false
	return d.array(func(index int) error {
		if index >= 64 {
			return errJSON
		}
		var kind, status string
		err := d.object(map[string]func() error{
			"type":   func() error { return d.stringInto(&kind, 128) },
			"status": func() error { return d.stringInto(&status, 32) },
		})
		if err != nil {
			return err
		}
		if kind != "MemoryPressure" {
			return nil
		}
		if seen || (status != "True" && status != "False" && status != "Unknown") {
			return errJSON
		}
		seen, value.context.MemoryPressure = true, status
		return nil
	})
}

func (d *decoder) resources(memory **uint64, pages *[]nodecontext.Hugepage, column resourceColumn) error {
	selected := 0
	return d.objectFields(func(name string) func() error {
		if name != "memory" && !strings.HasPrefix(name, "hugepages-") {
			return nil
		}
		return func() error {
			selected++
			if selected > nodecontext.MaxHugepages+1 || len(name) > nodecontext.MaxResourceBytes || len(validation.IsQualifiedName(name)) != 0 {
				return errJSON
			}
			text, err := d.text(128)
			if err != nil {
				return err
			}
			bytes, ok := resourcemetrics.QuantityBytes(text)
			if !ok {
				return errJSON
			}
			if name == "memory" {
				*memory = &bytes
				return nil
			}
			pageSize, valid := resourcemetrics.QuantityBytes(strings.TrimPrefix(name, "hugepages-"))
			if !valid || pageSize == 0 {
				return errJSON
			}
			for index := range *pages {
				if (*pages)[index].Resource == name {
					assignPage(&(*pages)[index], bytes, column)
					return nil
				}
			}
			if len(*pages) >= nodecontext.MaxHugepages {
				return errJSON
			}
			page := nodecontext.Hugepage{Resource: name}
			assignPage(&page, bytes, column)
			*pages = append(*pages, page)
			return nil
		}
	})
}

func assignPage(page *nodecontext.Hugepage, value uint64, column resourceColumn) {
	switch column {
	case capacityColumn:
		page.CapacityBytes = &value
	case allocatableColumn:
		page.AllocatableBytes = &value
	}
}

func (n nodeTarget) endpoint() string {
	return "https://" + net.JoinHostPort(n.ip, strconv.FormatUint(n.port, 10)) + "/stats/summary"
}
