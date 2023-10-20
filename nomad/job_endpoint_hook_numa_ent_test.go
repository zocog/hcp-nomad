// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

//go:build ent

package nomad

import (
	"testing"

	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/shoenig/test/must"
)

func Test_jobNumaHook_Validate(t *testing.T) {
	ci.Parallel(t)

	job := mock.Job()

	// forget to set cores resource
	job.TaskGroups[0].Tasks[0].Resources.NUMA = &structs.NUMA{
		Affinity: "require",
	}

	hook := jobNumaHook{}

	warnings, err := hook.Validate(job)
	must.SliceEmpty(t, warnings)
	must.EqError(t, err, "setting numa affinity also requires requesting cores")

	// now set the cores resource
	job.TaskGroups[0].Tasks[0].Resources.Cores = 4
	warnings, err = hook.Validate(job)
	must.SliceEmpty(t, warnings)
	must.NoError(t, err)
}

func Test_jobNumaHook_Mutate(t *testing.T) {
	ci.Parallel(t)
}
