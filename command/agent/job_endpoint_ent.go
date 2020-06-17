// +build ent

package agent

import (
	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/helper"
)

// regionForJob sets the region to global for multiregion jobs so that it can
// be interpolated in the RPC handler on region fan-out.
func regionForJob(job *api.Job, requestRegion *string) *string {
	if job.Multiregion != nil {
		return helper.StringToPtr("global")
	}

	return requestRegion
}
