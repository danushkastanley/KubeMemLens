package main

import (
	"errors"
	"flag"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/extension"
)

type replicaFlags struct {
	enabled    bool
	namespaces string
}

func registerReplicaFlags(flags *flag.FlagSet) *replicaFlags {
	f := &replicaFlags{}
	flags.BoolVar(&f.enabled, "replica-baselines", false, "enable informational comparisons of current authorised workload replicas")
	flags.StringVar(&f.namespaces, "replica-namespaces", "", "comma-separated namespaces allowed to use replica comparisons")
	return f
}

func (f *replicaFlags) resolve(mode string) (*extension.ReplicaOptions, error) {
	if !f.enabled {
		if f.namespaces != "" {
			return nil, errors.New("replica namespaces require replica-baselines")
		}
		return nil, nil
	}
	if mode != ingestionAuthenticated {
		return nil, errors.New("replica comparisons require authenticated ingestion")
	}
	options := &extension.ReplicaOptions{Namespaces: strings.Split(f.namespaces, ",")}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	return options, nil
}
