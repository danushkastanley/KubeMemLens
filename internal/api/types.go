package api

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

type AgentSnapshot struct {
	NodeName   string              `json:"nodeName"`
	CapturedAt time.Time           `json:"capturedAt"`
	Containers []ContainerSnapshot `json:"containers"`
}

type ContainerSnapshot struct {
	Namespace     string                `json:"namespace"`
	PodName       string                `json:"podName"`
	PodUID        string                `json:"podUID"`
	ContainerName string                `json:"containerName"`
	ContainerID   string                `json:"containerID"`
	NodeName      string                `json:"nodeName"`
	CgroupPath    string                `json:"cgroupPath"`
	CapturedAt    time.Time             `json:"capturedAt"`
	Memory        model.MemoryBreakdown `json:"memory"`
}

type PodSnapshot struct {
	Namespace  string                `json:"namespace"`
	PodName    string                `json:"podName"`
	PodUID     string                `json:"podUID"`
	NodeName   string                `json:"nodeName"`
	CapturedAt time.Time             `json:"capturedAt"`
	Containers []ContainerSnapshot   `json:"containers"`
	Memory     model.MemoryBreakdown `json:"memory"`
}

type NamespaceSnapshot struct {
	Namespace  string                `json:"namespace"`
	CapturedAt time.Time             `json:"capturedAt"`
	PodCount   int                   `json:"podCount"`
	Memory     model.MemoryBreakdown `json:"memory"`
}

type SnapshotPostResponse struct {
	OK         bool `json:"ok"`
	Containers int  `json:"containers"`
}

type DebugStore struct {
	TotalContainers int `json:"totalContainers"`
	StaleContainers int `json:"staleContainers"`
	Pods            int `json:"pods"`
	Namespaces      int `json:"namespaces"`
}

type StoreDebug = DebugStore
