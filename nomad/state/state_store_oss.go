// +build !ent

package state

import (
	"github.com/hashicorp/nomad/nomad/structs"
)

// enterpriseInit is used to initialize the state store with enterprise
// objects.
func (s *StateStore) enterpriseInit() error {
	return nil
}

// namespaceExists returns whether a namespace exists
func (s *StateStore) namespaceExists(txn *txn, namespace string) (bool, error) {
	return namespace == structs.DefaultNamespace, nil
}

// updateEntWithAlloc is used to update Nomad Enterprise objects when an allocation is
// added/modified/deleted
func (s *StateStore) updateEntWithAlloc(index uint64, new, existing *structs.Allocation, txn *txn) error {
	return nil
}

func (s *StateStore) NamespaceNames() ([]string, error) {
	return []string{structs.DefaultNamespace}, nil
}

// deleteRecommendationsByJob deletes all recommendations for the specified job
func (s *StateStore) deleteRecommendationsByJob(index uint64, txn Txn, job *structs.Job) error {
	return nil
}

// deleteJobPinnedRecommendations deletes all recommendations for the current version of a job
func (s *StateStore) deleteJobPinnedRecommendations(index uint64, txn Txn, job *structs.Job) error {
	return nil
}
