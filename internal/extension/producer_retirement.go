package extension

import (
	"sort"
	"time"
)

// Capacity pressure must not make a deleted Node's identities permanent. A
// fresh inventory can retire them without resetting another live stream.
func (c *Coordinator) pruneInactive(now time.Time) {
	identities, known := c.store.CurrentNodeIdentities(now)
	if !known {
		return
	}
	keys := make([]string, 0, len(c.agents))
	for key, state := range c.agents {
		if identities[state.nodeName] != state.nodeUID {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if len(c.retired) >= c.opts.MaxRetired {
			return
		}
		delete(c.nodes, c.agents[key].nodeKey)
		delete(c.agents, key)
		c.retired[key] = now.Add(c.opts.RetiredTTL)
	}
}
