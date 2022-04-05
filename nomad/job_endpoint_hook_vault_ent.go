//go:build ent
// +build ent

package nomad

import (
	"fmt"
	"strings"

	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/helper"
	"github.com/hashicorp/nomad/nomad/structs"
	vapi "github.com/hashicorp/vault/api"
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
