package main

import (
	"errors"
	"flag"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/extension"
)

type incidentSessionFlags struct {
	enabled    bool
	namespaces string
}

func registerIncidentSessionFlags(flags *flag.FlagSet) *incidentSessionFlags {
	f := &incidentSessionFlags{}
	flags.BoolVar(&f.enabled, "incident-sessions", false, "enable bounded owner-only incident sessions held in memory for one hour")
	flags.StringVar(&f.namespaces, "incident-session-namespaces", "", "comma-separated namespaces allowed to create incident sessions")
	return f
}

func (f *incidentSessionFlags) resolve(mode string) (*extension.IncidentSessionOptions, error) {
	if !f.enabled {
		if f.namespaces != "" {
			return nil, errors.New("incident session namespaces require incident-sessions")
		}
		return nil, nil
	}
	if mode != ingestionAuthenticated {
		return nil, errors.New("incident sessions require authenticated ingestion")
	}
	options := &extension.IncidentSessionOptions{Namespaces: strings.Split(f.namespaces, ",")}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	return options, nil
}
