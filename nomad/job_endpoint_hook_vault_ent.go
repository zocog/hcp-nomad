//go:build ent
// +build ent

package nomad

import (
	"fmt"
	"sort"
	"strings"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/hashicorp/go-set"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/helper"
	"github.com/hashicorp/nomad/nomad/structs"
	vapi "github.com/hashicorp/vault/api"
	"github.com/ryanuber/go-glob"
)

// validateNamespaces returns an error if the job contains multiple Vault
// namespaces.
func (h jobVaultHook) validateNamespaces(
	blocks map[string]map[string]*structs.Vault,
	token *vapi.Secret,
) error {

	// enterprise license enforcement - if not licensed then users can't
	// use multiple vault namespaces.
	err := h.srv.EnterpriseState.FeatureCheck(license.FeatureMultiVaultNamespaces, true)
	if err != nil {
		return err
	}

	requestedNamespaces := structs.VaultNamespaceSet(blocks)
	// Short circuit if no namespaces
	if len(requestedNamespaces) < 1 {
		return nil
	}

	var tokenData structs.VaultTokenData
	if err := structs.DecodeVaultSecretData(token, &tokenData); err != nil {
		return fmt.Errorf("failed to parse Vault token data: %v", err)
	}

	// If policy has a namespace check if the policy
	// is scoped correctly for any of the tasks namespaces.
	offending := helper.CheckNamespaceScope(tokenData.NamespacePath, requestedNamespaces)
	if offending != nil {
		return fmt.Errorf("Passed Vault token doesn't allow access to the following namespaces: %s", strings.Join(offending, ", "))
	}
	return nil
}

// validateClustersForNamespace verifies that all Vault blocks request a cluster
// permitted by the namespace configuration
func (h jobVaultHook) validateClustersForNamespace(job *structs.Job, blocks map[string]map[string]*structs.Vault) error {
	ns, err := h.srv.State().NamespaceByName(nil, job.Namespace)
	if err != nil {
		return err
	}
	if ns == nil {
		return fmt.Errorf("job %q is in nonexistent namespace %q", job.ID, job.Namespace)
	}
	if ns.VaultConfiguration == nil {
		return nil
	}

	clusters := set.New[string](0)
	for _, tg := range blocks {
		for _, vault := range tg {
			clusters.Insert(vault.Cluster)
		}
	}

	if clusters.Size() == 0 {
		return nil
	}
	if clusters.Size() > 1 || clusters.Slice()[0] != "default" {
		// enterprise license enforcement - if not licensed then users can't use
		// multiple Vault clusters or control access to them via namespaces
		err := h.srv.EnterpriseState.FeatureCheck(license.FeatureMultiVaultClusters, true)
		if err != nil {
			return err
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
		return merr.Errors[0] // don't ugly-wrap single errors
	}
	return merr.ErrorOrNil()
}

// validateClusterForNamespace verifies that a Vault block requests a cluster
// permitted by the namespace configuration
func (h jobVaultHook) validateClusterForNamespace(ns *structs.Namespace, cluster string) error {
	allowed := true
	switch {
	case ns.VaultConfiguration.Default == cluster:
		// Check for an exact match first to prevent a potential denied pattern
		// match. Jobs are always allowed to use the default cluster and
		// namespaces can't deny the default cluster.
		allowed = true

	case ns.VaultConfiguration.Allowed != nil:
		// Deny by default if namespace only allow certain clusters. An empty
		// allow list denies all clusters.
		allowed = false
		for _, allows := range ns.VaultConfiguration.Allowed {
			if glob.Glob(allows, cluster) {
				allowed = true
				break
			}
		}

	case len(ns.VaultConfiguration.Denied) > 0:
		for _, denies := range ns.VaultConfiguration.Denied {
			if glob.Glob(denies, cluster) {
				allowed = false
				break
			}
		}
	}

	if !allowed {
		return fmt.Errorf("namespace %q does not allow jobs to use vault cluster %q",
			ns.Name, cluster)
	}

	return nil
}

func (j jobVaultHook) Mutate(job *structs.Job) (*structs.Job, []error, error) {
	ns, err := j.srv.State().NamespaceByName(nil, job.Namespace)
	if err != nil {
		return nil, nil, err
	}
	if ns == nil {
		return nil, nil, fmt.Errorf("job %q is in nonexistent namespace %q", job.ID, job.Namespace)
	}
	if ns.VaultConfiguration == nil {
		return job, nil, nil // nothing to mutate with
	}

	for _, tg := range job.TaskGroups {
		for _, task := range tg.Tasks {
			if task.Vault == nil || task.Vault.Cluster != "" {
				continue
			}
			task.Vault.Cluster = ns.VaultConfiguration.Default
		}
	}

	return job, nil, nil
}
