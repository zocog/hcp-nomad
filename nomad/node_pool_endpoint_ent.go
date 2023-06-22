//go:build ent
// +build ent

package nomad

import (
	"github.com/hashicorp/nomad/nomad/structs"
)

func (n *NodePool) validateLicense(pool *structs.NodePool) error {
	return nil
}
