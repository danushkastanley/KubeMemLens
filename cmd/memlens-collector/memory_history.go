package main

import (
	"errors"
	"flag"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/extension"
)

type memoryHistoryFlags struct {
	enabled    bool
	namespaces string
	options    extension.MemoryHistoryOptions
}

func registerMemoryHistoryFlags(flags *flag.FlagSet) *memoryHistoryFlags {
	f := &memoryHistoryFlags{}
	flags.BoolVar(&f.enabled, "remote-history-enabled", false, "enable optional source-labelled memory history reads")
	flags.StringVar(&f.options.Endpoint, "remote-history-url", "", "fixed HTTPS Prometheus-compatible query endpoint")
	flags.StringVar(&f.options.Cluster, "remote-history-cluster", "", "required exact cluster label in remote history")
	flags.StringVar(&f.options.CAFile, "remote-history-ca-file", "", "required PEM trust bundle for remote history")
	flags.StringVar(&f.options.BearerTokenFile, "remote-history-token-file", "", "optional mounted read-only Prometheus bearer credential")
	flags.StringVar(&f.namespaces, "remote-history-namespaces", "", "comma-separated namespaces allowed to use memory history")
	flags.BoolVar(&f.options.Nodes, "remote-history-nodes", false, "allow separately authorised Node memory history reads")
	flags.BoolVar(&f.options.Workloads, "remote-history-workloads", false, "resolve current workload members for memory history")
	flags.BoolVar(&f.options.Markers, "memory-change-markers", false, "enable bounded authorised workload change context for history")
	return f
}

func (f *memoryHistoryFlags) resolve(mode string) (*extension.MemoryHistoryOptions, error) {
	if !f.enabled {
		if f.options.Endpoint != "" || f.options.Cluster != "" || f.options.CAFile != "" || f.options.BearerTokenFile != "" || f.namespaces != "" || f.options.Nodes || f.options.Workloads || f.options.Markers {
			return nil, errors.New("remote history configuration requires remote-history-enabled")
		}
		return nil, nil
	}
	if mode != ingestionAuthenticated {
		return nil, errors.New("remote history requires authenticated ingestion")
	}
	o := f.options
	if f.namespaces != "" {
		o.Namespaces = strings.Split(f.namespaces, ",")
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	return &o, nil
}
