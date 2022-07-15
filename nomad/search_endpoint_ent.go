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
	case structs.SecureVariables:
		return state.TableSecureVariables
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

func sufficientSearchPermsEnt(aclObj *acl.ACL) bool {
	return aclObj.AllowQuotaRead()
}

// filteredSearchContextsEnt returns ok if the alcObj is valid for a
// given enterprise context
func filteredSearchContextsEnt(aclObj *acl.ACL, namespace string, context structs.Context) bool {
	jobRead := aclObj.AllowNsOp(namespace, acl.NamespaceCapabilityReadJob)
	recRead := jobRead ||
		aclObj.AllowNsOp(namespace, acl.NamespaceCapabilitySubmitJob) ||
		aclObj.AllowNsOp(namespace, acl.NamespaceCapabilitySubmitRecommendation)

	switch context {
	case structs.Quotas:
		return aclObj.AllowQuotaRead()
	case structs.Recommendations:
		return recRead
	}
	return true
}
