package extension

import (
	"fmt"
	"net/http"
	"reflect"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"k8s.io/apiserver/pkg/authentication/user"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

// ProducerRole comes only from the configured authenticated ServiceAccount.
// The zero value preserves the established cgroup producer contract.
type ProducerRole uint8

const (
	CgroupProducer ProducerRole = iota
	NodeContextProducer
)

func producerClaims(info user.Info, cgroupUsername, nodeContextUsername string) (AgentClaims, error) {
	if nodeContextUsername != "" && info != nil && info.GetName() == nodeContextUsername {
		claims, err := claimsFromUser(info, nodeContextUsername)
		claims.Role = NodeContextProducer
		return claims, err
	}
	return claimsFromUser(info, cgroupUsername)
}

func (h *Handler) producerClaims(r *http.Request) (AgentClaims, error) {
	principal, ok := apirequest.UserFrom(r.Context())
	if !ok {
		return AgentClaims{}, fmt.Errorf("authenticated producer identity is required")
	}
	return producerClaims(principal, h.opts.AgentUsername, h.opts.NodeContextUsername)
}

func (c AgentClaims) instanceKey() string {
	if c.Role == CgroupProducer {
		return c.PodUID
	}
	return "node-context\x00" + c.PodUID
}

func (c AgentClaims) nodeKey() string {
	return fmt.Sprintf("%d\x00%s", c.Role, c.NodeUID)
}

func validateProducerSnapshot(claims AgentClaims, snapshot api.AgentSnapshot) error {
	switch claims.Role {
	case CgroupProducer:
		if snapshot.NodeContext != nil {
			return fmt.Errorf("cgroup producers cannot submit Node observations")
		}
	case NodeContextProducer:
		value := snapshot.NodeContext
		environment := snapshot.Environment
		environment.ContainerRuntimes = nil
		if snapshot.SchemaVersion < 3 || value == nil || len(snapshot.Containers) != 0 || len(snapshot.Environment.ContainerRuntimes) != 0 || !reflect.DeepEqual(environment, api.NodeEnvironment{}) {
			return fmt.Errorf("Node-context producers must submit only a schema 3 Node observation")
		}
		if value.NodeName != claims.NodeName || value.NodeUID != claims.NodeUID || !value.ReportedAt.Equal(snapshot.CapturedAt) {
			return fmt.Errorf("nested Node observation does not match the authenticated stream")
		}
	default:
		return fmt.Errorf("producer role is invalid")
	}
	return nil
}
