//go:build ent
// +build ent

package structs

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
)

// Validate returns an error if the node pool scheduler configuration is
// invalid.
func (n *NodePoolSchedulerConfiguration) Validate() error {
	if n == nil {
		return nil
	}

	var mErr *multierror.Error

	switch n.SchedulerAlgorithm {
	case "", SchedulerAlgorithmBinpack, SchedulerAlgorithmSpread:
	default:
		mErr = multierror.Append(mErr, fmt.Errorf("invalid scheduler algorithm %q", n.SchedulerAlgorithm))
	}

	return mErr.ErrorOrNil()
}
