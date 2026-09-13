package azdo

import (
	"fmt"
	"net/url"
	"sync"
)

// WorkItemState is one state in a work item type's workflow, e.g. "Active" or
// "Resolved". It is what the state picker offers: every state the type's
// process defines, not just the ones reachable from where an item happens to
// sit today — Azure DevOps itself is the one that knows which transitions are
// legal, and SetState's caller finds that out from the rejection.
type WorkItemState struct {
	Name string
}

// stateCache holds each work item type's states for the life of the process,
// guarded by a mutex because the fetch runs inside a tea.Cmd closure — off
// the UI goroutine — and two views (the list and an item pushed from it) can
// each open the picker for the same type at once.
//
// The cache never needs invalidating for a boardwalk session: a type's set of
// states comes from the process template the project is configured with, and
// changing that is an administrative action in Azure DevOps, not something
// that happens while someone is browsing work items.
type stateCache struct {
	mu    sync.Mutex
	byTyp map[string][]WorkItemState
}

// States returns the states configured for typ, for example "New", "Active",
// "Resolved" and "Closed" for a Bug. The result is cached per type for the
// session — see stateCache — so only the first picker opened on a given type
// costs a round trip.
func (c *Client) States(typ string) ([]WorkItemState, error) {
	c.states.mu.Lock()
	if cached, ok := c.states.byTyp[typ]; ok {
		c.states.mu.Unlock()
		return cached, nil
	}
	c.states.mu.Unlock()

	var resp struct {
		Value []struct {
			Name string `json:"name"`
		} `json:"value"`
	}
	endpoint := fmt.Sprintf("%s/%s/%s/_apis/wit/workitemtypes/%s/states?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), url.PathEscape(typ), APIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return nil, err
	}

	states := make([]WorkItemState, 0, len(resp.Value))
	for _, v := range resp.Value {
		states = append(states, WorkItemState{Name: v.Name})
	}

	c.states.mu.Lock()
	if c.states.byTyp == nil {
		c.states.byTyp = make(map[string][]WorkItemState)
	}
	c.states.byTyp[typ] = states
	c.states.mu.Unlock()

	return states, nil
}
