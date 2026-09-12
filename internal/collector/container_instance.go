package collector

import "github.com/danushkastanley/kube-memlens/internal/api"

// A runtime ID alone cannot transfer cumulative evidence between Pod lifetimes.
func sameContainerInstance(a, b api.ContainerSnapshot) bool {
	return a.ContainerID == b.ContainerID && a.PodUID == b.PodUID &&
		a.Namespace == b.Namespace && a.PodName == b.PodName &&
		a.ContainerName == b.ContainerName && a.CgroupPath == b.CgroupPath
}
