//go:build ent
// +build ent

package nomad

import (
	"github.com/hashicorp/go-msgpack/codec"
	"github.com/hashicorp/raft"
)

// registerLogAppliers registers all the Nomad Enterprise and Pro Raft log appliers.
func (n *nomadFSM) registerLogAppliers() {
	n.registerEntLogAppliers()
}

// registerSnapshotRestorers registers all the Nomad Enterprise and Pro snapshot
// restorers.
func (n *nomadFSM) registerSnapshotRestorers() {
	n.registerEntSnapshotRestorers()
}

// persistEnterpriseTables persists all the Nomad Enterprise and Pro state store tables.
func (s *nomadSnapshot) persistEnterpriseTables(sink raft.SnapshotSink, encoder *codec.Encoder) error {
	if err := s.persistEntTables(sink, encoder); err != nil {
		return err
	}
	return nil
}
