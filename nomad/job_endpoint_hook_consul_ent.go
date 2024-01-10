//go:build ent
// +build ent

package nomad

import (
	"fmt"
	"sort"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/hashicorp/go-set/v2"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/ryanuber/go-glob"
)

func (h jobConsulHook) Validate(job *structs.Job) ([]error, error) {

	ns, err := h.srv.State().NamespaceByName(nil, job.Namespace)
	if err != nil {
		return nil, err
	}
	if ns == nil {
		return nil, fmt.Errorf("job %q is in nonexistent namespace %q", job.ID, job.Namespace)
	}
	if ns.ConsulConfiguration == nil {
		return nil, nil
	}

	clusters := set.New[string](0)
	for _, group := range job.TaskGroups {

		groupPartition := ""

		if group.Consul != nil {
			groupPartition = group.Consul.Partition
			clusters.Insert(group.Consul.Cluster)
		}

		for _, service := range group.Services {
			if service.IsConsul() {
				clusters.Insert(service.Cluster)
			}
		}

		for _, task := range group.Tasks {
			for _, service := range task.Services {
				if service.IsConsul() {
					clusters.Insert(service.Cluster)
				}
			}

			if task.Consul != nil {
				err := h.validateTaskPartitionMatchesGroup(groupPartition, task.Consul)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	if clusters.Size() == 0 {
		return nil, nil
	}
	if clusters.Size() > 1 || clusters.Slice()[0] != "default" {
		// enterprise license enforcement - if not licensed then users can't use
		// multiple Consul clusters or control access to them via namespaces
		err := h.srv.EnterpriseState.FeatureCheck(license.FeatureMultiConsulClusters, true)
		if err != nil {
			return nil, err
		}
	}

	var merr *multierror.Error
	clusterNames := clusters.Slice()
	sort.Strings(clusterNames)
	for _, cluster := range clusterNames {
		err := h.validateClusterForNamespace(ns, cluster)
		if err != nil {
			merr = multierror.Append(merr, err)
		}
	}

	if merr != nil && merr.Len() == 1 {
		return nil, merr.Errors[0] // don't ugly-wrap single errors
	}
	return nil, merr.ErrorOrNil()
}

// validateClusterForNamespace verifies that a Consul block requests a cluster
// permitted by the namespace configuration
func (h jobConsulHook) validateClusterForNamespace(ns *structs.Namespace, cluster string) error {
	allowed := true
	switch {
	case ns.ConsulConfiguration.Default == cluster:
		// Check for an exact match first to prevent a potential denied pattern
		// match. Jobs are always allowed to use the default cluster and
		// namespaces can't deny the default cluster.
		allowed = true

	case ns.ConsulConfiguration.Allowed != nil:
		// Deny by default if namespace only allow certain clusters. An empty
		// allow list denies all clusters.
		allowed = false
		for _, allows := range ns.ConsulConfiguration.Allowed {
			if glob.Glob(allows, cluster) {
				allowed = true
				break
			}
		}

	case len(ns.ConsulConfiguration.Denied) > 0:
		for _, denies := range ns.ConsulConfiguration.Denied {
			if glob.Glob(denies, cluster) {
				allowed = false
				break
			}
		}
	}
	if !allowed {
		return fmt.Errorf("namespace %q does not allow jobs to use consul cluster %q",
			ns.Name, cluster)
	}

	return nil
}

func consulPartitionConstraint(cluster, partition string) *structs.Constraint {
	if cluster == structs.ConsulDefaultCluster || cluster == "" {
		return &structs.Constraint{
			LTarget: "${attr.consul.partition}",
			RTarget: partition,
			Operand: "=",
		}
	}
	return &structs.Constraint{
		LTarget: "${attr.consul." + cluster + ".partition}",
		RTarget: partition,
		Operand: "=",
	}
}

// Mutate ensures that the job's Consul cluster has been configured to be the
// default Consul cluster
func (j jobConsulHook) Mutate(job *structs.Job) (*structs.Job, []error, error) {

	ns, err := j.srv.State().NamespaceByName(nil, job.Namespace)
	if err != nil {
		return nil, nil, err
	}
	if ns == nil {
		return nil, nil, fmt.Errorf("job %q is in nonexistent namespace %q", job.ID, job.Namespace)
	}
	if ns.ConsulConfiguration == nil {
		return job, nil, nil // nothing to mutate with
	}

	defaultCluster := ns.ConsulConfiguration.Default
	if defaultCluster == "" {
		defaultCluster = "default" // shouldn't happen because of canonicalization
	}

	for _, group := range job.TaskGroups {
		if group.Consul != nil {
			if group.Consul.Cluster == "" {
				group.Consul.Cluster = structs.ConsulDefaultCluster
			}
			if group.Consul.Partition != "" {
				group.Constraints = append(group.Constraints,
					consulPartitionConstraint(group.Consul.Cluster, group.Consul.Partition))
			}
		}

		for _, service := range group.Services {
			if service.IsConsul() {
				if service.Cluster == "" {
					service.Cluster = defaultCluster
				}
			}
		}

		for _, task := range group.Tasks {
			if task.Consul != nil {
				if task.Consul.Cluster == "" {
					task.Consul.Cluster = structs.ConsulDefaultCluster
				}
				if task.Consul.Partition != "" {
					task.Constraints = append(task.Constraints,
						consulPartitionConstraint(task.Consul.Cluster, task.Consul.Partition))
				}
			}

			for _, service := range task.Services {
				if service.IsConsul() {
					if service.Cluster == "" {
						service.Cluster = defaultCluster
					}
				}
			}
		}
	}

	return job, nil, nil
}
