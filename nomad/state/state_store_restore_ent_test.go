//go:build ent
// +build ent

package state

import (
	"testing"

	memdb "github.com/hashicorp/go-memdb"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/shoenig/test/must"
)

func TestStateStore_RestoreNamespace_NodePool(t *testing.T) {
	ci.Parallel(t)

	state := testStateStore(t)
	ns := mock.Namespace()

	// Simulate a namespace pre-1.6.0 or from OSS.
	ns.NodePoolConfiguration = nil
	ns.SetHash()

	restore, err := state.Restore()
	must.NoError(t, err)

	must.NoError(t, restore.NamespaceRestore(ns))
	restore.Commit()

	ws := memdb.NewWatchSet()
	out, err := state.NamespaceByName(ws, ns.Name)
	must.NoError(t, err)
	must.NotNil(t, out.NodePoolConfiguration)
}
