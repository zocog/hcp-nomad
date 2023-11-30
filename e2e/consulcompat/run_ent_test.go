//go:build ent

package consulcompat

import (
	"testing"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-version"
	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/testutil"
	"github.com/shoenig/test/must"
)

// usable is used by the downloader to verify that we're getting the right
// versions of Consul ENT
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

func testConsulBuildLegacy(t *testing.T, b build, baseDir string) {
	t.Run("consul-legacy("+b.Version+")", func(t *testing.T) {
		consulHTTPAddr, consulAPI := startConsul(t, b, baseDir, "")

		// smoke test before we continue
		verifyConsulVersion(t, consulAPI, b.Version)

		setupConsulNamespace(t, consulAPI)

		// we need an ACL policy that allows the Nomad agent to fingerprint
		// Consul, register services, render templates, and mint new SI tokens
		consulToken := setupConsulACLsForServices(t, consulAPI,
			"./input/consul-policy-for-nomad-legacy.hcl")

		// we need service intentions so Connect apps can reach each other
		setupConsulServiceIntentions(t, consulAPI)

		// note: Nomad needs to be live before we can setupConsul because we
		// need it up to serve the JWKS endpoint

		consulCfg := &testutil.Consul{
			Name:    "default",
			Address: consulHTTPAddr,
			Auth:    "",
			Token:   consulToken,
		}

		nc := startNomad(t, consulCfg)
		verifyConsulFingerprint(t, nc, b.Version, "default")

		nc.Namespaces().Register(&api.Namespace{
			Name:        "prod",
			Description: "namespace for production workloads",
			ConsulConfiguration: &api.NamespaceConsulConfiguration{
				Default: "default",
				Allowed: []string{"default"},
			},
		}, nil)

		runConnectJob(t, nc, "prod", "./input/connect.nomad-ent.hcl")
	})
}

func testConsulBuild(t *testing.T, b build, baseDir string) {
	t.Run("consul("+b.Version+")", func(t *testing.T) {
		consulHTTPAddr, consulAPI := startConsul(t, b, baseDir, "")

		// smoke test before we continue
		verifyConsulVersion(t, consulAPI, b.Version)

		setupConsulNamespace(t, consulAPI)

		// we need an ACL policy that only allows the Nomad agent to fingerprint
		// Consul and register itself, and set up service intentions
		//
		// Note that with this policy we must use Workload Identity for Connect
		// jobs, or we'll get "failed to derive SI token" errors from the client
		// because the Nomad agent's token doesn't have "acl:write"
		consulToken := setupConsulACLsForServices(t, consulAPI,
			"./input/consul-policy-for-nomad.hcl")

		// we need service intentions so Connect apps can reach each other, and
		// an ACL role and policy that tasks will be able to use to render
		// templates
		setupConsulServiceIntentions(t, consulAPI)
		setupConsulACLsForTasks(t, consulAPI,
			"nomad-prod", "./input/consul-policy-for-tasks.hcl")

		// note: Nomad needs to be live before we can setup Consul auth methods
		// because we need it up to serve the JWKS endpoint

		consulCfg := &testutil.Consul{
			Name:                      "default",
			Address:                   consulHTTPAddr,
			Auth:                      "",
			Token:                     consulToken,
			ServiceIdentityAuthMethod: "nomad-workloads",
			ServiceIdentity: &testutil.WorkloadIdentityConfig{
				Audience: []string{"consul.io"},
				TTL:      "1h",
			},
			TaskIdentityAuthMethod: "nomad-workloads",
			TaskIdentity: &testutil.WorkloadIdentityConfig{
				Audience: []string{"consul.io"},
				TTL:      "1h",
			},
		}

		nc := startNomad(t, consulCfg)

		// configure authentication for WI to Consul
		setupConsulJWTAuth(t, consulAPI, nc.Address(), []*consulapi.ACLAuthMethodNamespaceRule{{
			Selector:      `"nomad_service" in value`,
			BindNamespace: "${value.consul_namespace}",
		}})

		verifyConsulFingerprint(t, nc, b.Version, "default")

		nc.Namespaces().Register(&api.Namespace{
			Name:        "prod",
			Description: "namespace for production workloads",
			ConsulConfiguration: &api.NamespaceConsulConfiguration{
				Default: "default",
				Allowed: []string{"default"},
			},
		}, nil)

		runConnectJob(t, nc, "prod", "./input/connect.nomad-ent.hcl")
	})
}

// setupConsulNamespace installs a "prod" namespace in Consul
func setupConsulNamespace(t *testing.T, consulAPI *consulapi.Client) {
	_, _, err := consulAPI.Namespaces().Create(&consulapi.Namespace{
		Name:        "prod",
		Description: "Consul namespace for Nomad prod workloads",
	}, nil)
	must.NoError(t, err, must.Sprint("could not set up Consul namespace"))
}
