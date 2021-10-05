//go:build ent
// +build ent

package nomad

import (
	"fmt"

	memdb "github.com/hashicorp/go-memdb"
	"github.com/hashicorp/nomad/acl"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"
)

var (
	// allContexts are the available contexts which are searched to find matches
	// for a given prefix
	allContexts = append(ossContexts, entContexts...)

	// entContexts are the ent contexts which are searched to find matches
	// for a given prefix
	entContexts = []structs.Context{structs.Quotas, structs.Recommendations}
)

// contextToIndex returns the index name to lookup in the state store.
func contextToIndex(ctx structs.Context) string {
	switch ctx {
	case structs.Quotas:
		return state.TableQuotaSpec
	case structs.Recommendations:
		return state.TableRecommendations
	default:
		return string(ctx)
	}
}

// getEnterpriseMatch is used to match on an object only defined in Nomad Pro or
// Premium
func getEnterpriseMatch(match interface{}) (id string, ok bool) {
	switch m := match.(type) {
	case *structs.QuotaSpec:
		return m.Name, true
	case *structs.Recommendation:
		return m.ID, true
	default:
		return "", false
	}
}

// getEnterpriseResourceIter is used to retrieve an iterator over an enterprise
// only table.
func getEnterpriseResourceIter(context structs.Context, aclObj *acl.ACL, namespace, prefix string, ws memdb.WatchSet, state *state.StateStore) (memdb.ResultIterator, error) {
	switch context {
	case structs.Quotas:
		return state.QuotaSpecsByNamePrefix(ws, prefix)
	case structs.Recommendations:
		return state.RecommendationsByIDPrefixIter(ws, namespace, prefix)
	default:
		return nil, fmt.Errorf("context must be one of %v or 'all' for all contexts; got %q", allContexts, context)
	}
}

// getEnterpriseFuzzyResourceIter is used to retrieve an iterator over an enterprise
// only table.
func getEnterpriseFuzzyResourceIter(context structs.Context, aclObj *acl.ACL, namespace string, ws memdb.WatchSet, state *state.StateStore) (memdb.ResultIterator, error) {
	// Currently no enterprise objects are fuzzy searchable, deferring to prefix search.
	// If we do add fuzzy searchable enterprise objects, be sure to follow the wildcard
	// pattern to get filtering iterators in getFuzzyResourceIterator.
	return nil, fmt.Errorf("context must be one of %v or 'all' for all contexts; got %q", allContexts, context)
}

// sufficientSearchPerms returns true if the provided ACL has access to any
// capabilities required for prefix searching.
//
// Returns true if aclObj is nil.
func sufficientSearchPerms(aclObj *acl.ACL, namespace string, context structs.Context) bool {
	if aclObj == nil {
		return true
	}

	nodeRead := aclObj.AllowNodeRead()
	allowNS := aclObj.AllowNamespace(namespace)
	allowQuota := aclObj.AllowQuotaRead()
	jobRead := aclObj.AllowNsOp(namespace, acl.NamespaceCapabilityReadJob)
	if !nodeRead && !allowNS && !allowQuota && !jobRead {
		return false
	}

	// Reject requests that explicitly specify a disallowed context. This
	// should give the user better feedback then simply filtering out all
	// results and returning an empty list.
	if !nodeRead && context == structs.Nodes {
		return false
	}
	if !allowNS && context == structs.Namespaces {
		return false
	}
	if !allowQuota && context == structs.Quotas {
		return false
	}
	if !jobRead {
		switch context {
		case structs.Allocs, structs.Deployments, structs.Evals, structs.Jobs:
			return false
		}
	}

	return true
}

// filteredSearchContexts returns the contexts the aclObj is valid for. If aclObj is
// nil all contexts are returned.
func filteredSearchContexts(aclObj *acl.ACL, namespace string, context structs.Context) []structs.Context {
	var all []structs.Context

	switch context {
	case structs.All:
		all = make([]structs.Context, len(allContexts))
		copy(all, allContexts)
	default:
		all = []structs.Context{context}
	}

	// If ACLs aren't enabled return all contexts
	if aclObj == nil {
		return all
	}

	jobRead := aclObj.AllowNsOp(namespace, acl.NamespaceCapabilityReadJob)
	recRead := jobRead ||
		aclObj.AllowNsOp(namespace, acl.NamespaceCapabilitySubmitJob) ||
		aclObj.AllowNsOp(namespace, acl.NamespaceCapabilitySubmitRecommendation)

	// Filter contexts down to those the ACL grants access to
	available := make([]structs.Context, 0, len(all))
	for _, c := range all {
		switch c {
		case structs.Allocs, structs.Jobs, structs.Evals, structs.Deployments:
			if jobRead {
				available = append(available, c)
			}
		case structs.Namespaces:
			if aclObj.AllowNamespace(namespace) {
				available = append(available, c)
			}
		case structs.Nodes:
			if aclObj.AllowNodeRead() {
				available = append(available, c)
			}
		case structs.Quotas:
			if aclObj.AllowQuotaRead() {
				available = append(available, c)
			}
		case structs.Recommendations:
			if recRead {
				available = append(available, c)
			}
		}

	}
	return available
}
