// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build ent
// +build ent

package nomad

import (
	"fmt"

	"github.com/hashicorp/nomad/nomad/structs"

	"github.com/ryanuber/go-glob"
)

// enterpriseValidation implements any admission hooks for node pools for Nomad
// Enterprise.
func (j jobNodePoolValidatingHook) enterpriseValidation(job *structs.Job, _ *structs.NodePool) ([]error, error) {
	ns, err := j.srv.State().NamespaceByName(nil, job.Namespace)
	if err != nil {
		return nil, err
	}
	if ns == nil {
		return nil, fmt.Errorf("namespace %q not found", job.Namespace)
	}

	// Verify job is in a node pool that the namespace allows to be used.
	allowed := true
	switch {
	case ns.NodePoolConfiguration.Default == job.NodePool:
		// Check for an exact match first to prevent a potential denied
		// pattern match.
		// Jobs are always allowed to use the default node pool and namespaces
		// can't deny their default node pool.
		allowed = true

	case len(ns.NodePoolConfiguration.Allowed) > 0:
		// Deny by default if namespace only allow certain node pools.
		allowed = false
		for _, allows := range ns.NodePoolConfiguration.Allowed {
			if glob.Glob(allows, job.NodePool) {
				allowed = true
				break
			}
		}

	case len(ns.NodePoolConfiguration.Denied) > 0:
		for _, denies := range ns.NodePoolConfiguration.Denied {
			if glob.Glob(denies, job.NodePool) {
				allowed = false
				break
			}
		}
	}
	if !allowed {
		return nil, fmt.Errorf("namespace %q does not allow jobs to use node pool %q", ns.Name, job.NodePool)
	}

	return nil, nil
}

// jobNodePoolMutatingHook mutates the job on Nomad Enterprise only.
type jobNodePoolMutatingHook struct {
	srv *Server
}

func (j jobNodePoolMutatingHook) Name() string {
	return "node-pool-mutation"
}

func (j jobNodePoolMutatingHook) Mutate(job *structs.Job) (*structs.Job, []error, error) {
	if job.NodePool != "" {
		return job, nil, nil
	}

	// Set the job's node pool to the namespace default value.
	ns, err := j.srv.State().NamespaceByName(nil, job.Namespace)
	if err != nil {
		return nil, nil, err
	}
	if ns == nil {
		return nil, nil, fmt.Errorf("namespace %q not found", job.Namespace)
	}
	job.NodePool = ns.NodePoolConfiguration.Default

	return job, nil, nil
}
