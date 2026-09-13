module github.com/danushkastanley/kube-memlens/prototype/trace

go 1.27.1

require (
	github.com/cilium/ebpf v0.22.0
	github.com/danushkastanley/kube-memlens v0.0.0
	golang.org/x/sys v0.47.0
)

replace github.com/danushkastanley/kube-memlens => ../..
