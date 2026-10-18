package main

import (
	"context"
	"errors"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodebinding"
)

type installedRuntime interface {
	nodebinding.Runtime
	Close(context.Context) error
}

func validatePolicyArguments(path, digest string) error {
	if digest != "" && path == "" {
		return errors.New("policy digest requires an installation policy")
	}
	return nil
}
