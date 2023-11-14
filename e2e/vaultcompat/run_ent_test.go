//go:build ent

package vaultcompat

import (
	"context"
	"testing"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/nomad/api"
	vaultapi "github.com/hashicorp/vault/api"
	"github.com/shoenig/test/must"
)

// usable is used by the downloader to verify that we're getting the right
// versions of Vault ENT
func usable(v, minimum *version.Version) bool {
	switch {
	case v.Prerelease() != "":
		return false
	case v.Metadata() != "ent":
		return false
	case v.LessThan(minimum):
		return false
	default:
		return true
	}
}

func testVaultLegacy(t *testing.T, b build) {
	vStop, vc := startVault(t, b)
	defer vStop()

	vc.Logical().Write("/sys/namespaces/prod", nil)
	vc.SetNamespace("prod")
	setupVaultLegacy(t, vc)

	nStop, nc := startNomad(t, configureNomadVaultLegacy(vc))
	defer nStop()

	nc.Namespaces().Register(&api.Namespace{
		Name:        "prod",
		Description: "namespace for production workloads",
		VaultConfiguration: &api.NamespaceVaultConfiguration{
			Default: "default",
			Allowed: []string{"default"},
		},
	}, nil)

	runJob(t, nc, "input/cat_ent.hcl", "prod", validateLegacyAllocs)
}

func testVaultJWT(t *testing.T, b build) {
	vStop, vc := startVault(t, b)
	defer vStop()

	// Start Nomad without access to the Vault token.
	vaultToken := vc.Token()
	vc.SetToken("")
	nStop, nc := startNomad(t, configureNomadVaultJWT(vc))
	defer nStop()

	nc.Namespaces().Register(&api.Namespace{
		Name:        "prod",
		Description: "namespace for production workloads",
		VaultConfiguration: &api.NamespaceVaultConfiguration{
			Default: "default",
			Allowed: []string{"default"},
		},
	}, nil)

	// Restore token and configure Vault for JWT login.
	vc.SetToken(vaultToken)
	vc.Logical().Write("/sys/namespaces/prod", nil)
	vc.SetNamespace("prod")
	setupVaultJWT(t, vc, nc.Address()+"/.well-known/jwks.json")

	err := vc.Sys().Mount("secret", &vaultapi.MountInput{
		Type:        "kv-v2",
		Description: "kv2 for namespace prod",
	})
	must.NoError(t, err)

	// Write secrets for test job.
	_, err = vc.KVv2("secret").Put(context.Background(), "prod/cat_jwt", map[string]any{
		"secret": "workload",
	})
	must.NoError(t, err)

	_, err = vc.KVv2("secret").Put(context.Background(), "restricted", map[string]any{
		"secret": "restricted",
	})
	must.NoError(t, err)

	// Run test job.
	runJob(t, nc, "input/cat_ent_jwt.hcl", "prod", validateJWTAllocs)
}
