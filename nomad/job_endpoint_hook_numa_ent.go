// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

//go:build ent

package nomad

import (
	"errors"

	"github.com/hashicorp/nomad/nomad/structs"
)

func (jobNumaHook) Validate(job *structs.Job) ([]error, error) {
	for _, tg := range job.TaskGroups {
		for _, task := range tg.Tasks {
			if task.Resources.NUMA.Requested() {
				if task.Resources.Cores <= 0 {
					return nil, errors.New("setting numa affinity also requires requesting cores")
				}
			}
		}
	}
	// nothing to validate on enterprise; correctness of the resources.numa
	// block is taken care of in the jobspec validation path
	return nil, nil
}

func (jobNumaHook) Mutate(job *structs.Job) (*structs.Job, []error, error) {
	// implicit NUMA constraints are handled by job_endpoint_hooks.go
	return job, nil, nil
}
