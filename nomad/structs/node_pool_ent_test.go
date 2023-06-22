//go:build ent
// +build ent

package structs

import (
	"testing"

	"github.com/hashicorp/nomad/ci"
	"github.com/shoenig/test/must"
)

func TestNodePool_Validate_ENT(t *testing.T) {
	ci.Parallel(t)

	testCases := []struct {
		name        string
		pool        *NodePool
		expectedErr string
	}{
		{
			name: "scheduler algorithm binpack is allowed",
			pool: &NodePool{
				Name: "valid",
				SchedulerConfiguration: &NodePoolSchedulerConfiguration{
					SchedulerAlgorithm: SchedulerAlgorithmBinpack,
				},
			},
			expectedErr: "",
		},
		{
			name: "scheduler algorithm spread is allowed",
			pool: &NodePool{
				Name: "valid",
				SchedulerConfiguration: &NodePoolSchedulerConfiguration{
					SchedulerAlgorithm: SchedulerAlgorithmSpread,
				},
			},
			expectedErr: "",
		},
		{
			name: "scheduler algorithm not set is allowed",
			pool: &NodePool{
				Name:                   "valid",
				SchedulerConfiguration: &NodePoolSchedulerConfiguration{},
			},
			expectedErr: "",
		},
		{
			name: "invalid scheduling algorithm",
			pool: &NodePool{
				Name: "valid",
				SchedulerConfiguration: &NodePoolSchedulerConfiguration{
					SchedulerAlgorithm: "invalid",
				},
			},
			expectedErr: "invalid scheduler algorithm",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.pool.Validate()

			if tc.expectedErr != "" {
				must.ErrorContains(t, err, tc.expectedErr)
			} else {
				must.NoError(t, err)
			}
		})
	}
}
