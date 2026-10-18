package replicabaseline

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// Selection is request-local authorised metadata, not a public response. It
// retains no Pod labels, commands, environment values or free-form event text.
type Selection struct {
	Request         memoryhistory.Request
	Requested       changemarkers.Object
	Workload        changemarkers.Object
	Generation      int64
	ResolvedAt      time.Time
	EventState      changemarkers.Coverage
	EventsTruncated bool
	Members         []Member
}

type Member struct {
	Object          changemarkers.Object
	Owners          []changemarkers.Object
	Node, NodeUID   string
	Revision, Shape string
	Lifecycle       Lifecycle
	StableSince     time.Time
	Changes         ChangeState
	PodBudget       model.MemoryResourceBudget
	Containers      []Container
}

type Container struct {
	Name, Role, ID, ImageDigest string
	StartedAt, EndedAt          time.Time
	Configured                  model.MemoryResourceBudget
	Resources                   model.ContainerMemoryResources
}

type Resolver interface {
	Resolve(context.Context, memoryhistory.Request) (Selection, error)
}
