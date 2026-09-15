package main

import (
	"context"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodebinding"
)

type installedRuntime interface {
	nodebinding.Runtime
	Close(context.Context) error
}
