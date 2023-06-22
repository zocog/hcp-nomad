//go:build ent
// +build ent

package nomad

import (
	"github.com/hashicorp/nomad-licensing/license"

	"github.com/hashicorp/nomad/nomad/structs"
)

func (n *NodePool) validateLicense(pool *structs.NodePool) error {
	if pool != nil && pool.SchedulerConfiguration != nil {
		return n.srv.EnterpriseState.FeatureCheck(license.FeatureNodePoolsGovernance, true)
	}

	return nil
}
