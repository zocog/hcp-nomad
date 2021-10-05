//go:build ent
// +build ent

package consul

import (
	"fmt"
	"os"

	"github.com/hashicorp/nomad/e2e/consulacls"
	"github.com/hashicorp/nomad/e2e/e2eutil"
	"github.com/hashicorp/nomad/e2e/framework"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/stretchr/testify/require"
)

// ConsulNamespacesE2ETestACLs implements e2e tests around Consul Namespaces
// with ACLs enabled from Nomad Enterprise, using good and bad Consul ACL tokens
// of varying permissions.
type ConsulNamespacesE2ETestACLs struct {
	// ConsulNamespacesE2ETest is embedded and its tests are run using the
	// Consul global-management ACL token; (i.e. everything should work as the
	// token has total permission over Consul).
	ConsulNamespacesE2ETest

	// manageConsulACLs is used to 'enable' and 'disable' Consul ACLs in the
	// Consul Cluster that has been setup for e2e testing.
	manageConsulACLs consulacls.Manager

	// created policy and token IDs should be set here so they can be cleaned
	// up after each test case, organized by namespace
	policyIDs map[string][]string
	tokenIDs  map[string][]string
}

func (tc *ConsulNamespacesE2ETestACLs) addPolicy(namespace, policyID string) {
	tc.policyIDs[namespace] = append(tc.policyIDs[namespace], policyID)
}

func (tc *ConsulNamespacesE2ETestACLs) addToken(namespace, tokenID string) {
	tc.tokenIDs[namespace] = append(tc.tokenIDs[namespace], tokenID)
}

func (tc *ConsulNamespacesE2ETestACLs) BeforeAll(f *framework.F) {
	tc.policyIDs = make(map[string][]string)
	tc.tokenIDs = make(map[string][]string)

	e2eutil.WaitForLeader(f.T(), tc.Nomad())
	e2eutil.WaitForNodesReady(f.T(), tc.Nomad(), 1)

	// Now enable Consul ACLs, the bootstrapping process for which will be
	// managed automatically if needed.
	var err error
	tc.manageConsulACLs, err = consulacls.New(consulacls.DefaultTFStateFile)
	require.NoError(f.T(), err)
	tc.enableConsulACLs(f)

	// create a set of consul namespaces in which to register services
	e2eutil.CreateConsulNamespaces(f.T(), tc.Consul(), consulNamespaces)

	// insert a key of the same name into KV for each namespace, where the value
	// contains the namespace name making it easy to determine which namespace
	// consul template actually accessed
	for _, namespace := range allConsulNamespaces {
		value := fmt.Sprintf("ns_%s", namespace)
		e2eutil.PutConsulKey(f.T(), tc.Consul(), namespace, "ns-kv-example", value)
	}
}

// enableConsulACLs effectively executes `consul-acls-manage.sh enable`, which
// will activate Consul ACLs, going through the bootstrap process if necessary.
//
// The Consul client object will then get its X-Consul-Token header set to the
// generated Consul management token, so that e2eutil helper functions continue
// to work.
func (tc *ConsulNamespacesE2ETestACLs) enableConsulACLs(f *framework.F) {
	tc.cToken = tc.manageConsulACLs.Enable(f.T())
	tc.Consul().AddHeader("X-Consul-Token", tc.cToken)
}

// AfterAll runs after all tests are complete.
//
// We disable ConsulACLs in here to isolate the use of Consul ACLs only to
// test suites that explicitly want to test with them enabled.
//
// Remove the X-Consul-Token header from the Consul client object.
func (tc *ConsulNamespacesE2ETestACLs) AfterAll(f *framework.F) {
	tc.ConsulNamespacesE2ETest.AfterAll(f)
	tc.manageConsulACLs.Disable(f.T())
}

func (tc *ConsulNamespacesE2ETestACLs) AfterEach(f *framework.F) {
	if os.Getenv("NOMAD_TEST_SKIPCLEANUP") == "1" {
		return
	}

	// cleanup jobs
	for _, id := range tc.jobIDs {
		_, _, err := tc.Nomad().Jobs().Deregister(id, true, nil)
		f.NoError(err)
	}

	// cleanup consul tokens
	e2eutil.DeleteConsulTokens(f.T(), tc.Consul(), tc.tokenIDs)

	// cleanup consul policies
	e2eutil.DeleteConsulPolicies(f.T(), tc.Consul(), tc.policyIDs)

	// do garbage collection
	err := tc.Nomad().System().GarbageCollect()
	f.NoError(err)

	// reset accumulators
	tc.tokenIDs = make(map[string][]string)
	tc.policyIDs = make(map[string][]string)
}

// submitJob is used in tests that expect job submission to fail, due to the
// use of Consul tokens with insufficient or missing ACL policies. The error
// returned is coming from job.Register, and should be validated in the test.
func (tc *ConsulNamespacesE2ETestACLs) submitJob(f *framework.F, token, file string) error {
	job, parseErr := e2eutil.Parse2(f.T(), file)
	f.NoError(parseErr)

	if token != "" {
		job.ConsulToken = &token
	}

	jobAPI := tc.Nomad().Jobs()
	_, _, err := jobAPI.Register(job, nil)
	return err
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulRegisterGroupServiceMinimalToken(f *framework.F) {
	// create policy that can write services into banana, cherry, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
service_prefix "z" {policy = "write" }

namespace "banana" {
  service_prefix "b" { policy = "write" }
}

namespace_prefix "cher" {
  service_prefix "c" { policy = "write" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// run test with limited token
	tc.testConsulRegisterGroupServices(f, tokenID, "apple", "banana", "cherry", "default")
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulRegisterGroupServiceInsufficientToken(f *framework.F) {
	// create policy that can write services into banana, cherry, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
service_prefix "" {policy = "write" }
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// submit job with insufficient token and expect error
	err := tc.submitJob(f, tokenID, cnsJobGroupServices)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: insufficient Consul ACL permissions to write service`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulRegisterGroupServiceMissingToken(f *framework.F) {
	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobGroupServices)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulRegisterTaskServicesMinimalToken(f *framework.F) {
	// create policy that can write services into banana, cherry, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
service_prefix "z" {policy = "write" }

namespace "banana" {
  service_prefix "b" { policy = "write" }
}

namespace_prefix "cher" {
  service_prefix "c" { policy = "write" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// run test with limited token
	tc.testConsulRegisterTaskServices(f, tokenID, "apple", "banana", "cherry", "default")
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulRegisterTaskServicesInsufficientToken(f *framework.F) {
	// create policy that can write services into banana, cherry, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
service_prefix "z" {policy = "write" }
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// submit job with insufficient token and expect error
	err := tc.submitJob(f, tokenID, cnsJobTaskServices)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: insufficient Consul ACL permissions to write service`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulRegisterTaskServicesMissingToken(f *framework.F) {
	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobTaskServices)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulTemplateKVMinimalToken(f *framework.F) {
	// create policy that can write keys into default, banana
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
key_prefix "" { policy = "read" }

namespace "banana" {
  key_prefix "" { policy = "read" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// run test with limited token
	tc.testConsulTemplateKV(f, tokenID, "value: ns_banana", "value: ns_default")
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulTemplateKVInsufficientToken(f *framework.F) {
	// create policy that can write keys into default, banana
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
key_prefix "" { policy = "read" }

namespace "banana" {
  key_prefix "foo/" { policy = "read" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// submit job with insufficient token and expect error
	err := tc.submitJob(f, tokenID, cnsJobTemplateKV)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: insufficient Consul ACL permissions to use template`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulTemplateKVMissingToken(f *framework.F) {
	// submit job with without token and expect error
	err := tc.submitJob(f, "", cnsJobTemplateKV)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulConnectSidecarsMinimalToken(f *framework.F) {
	// create policy that can write services into apple, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
service "count-api-z" { policy = "write" }
service "count-dashboard-z" { policy = "write" }

namespace "apple" {
  service "count-api" { policy = "write" }
  service "count-dashboard" { policy = "write" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// run test with limited token
	tc.testConsulConnectSidecars(f, tokenID, "apple", "default")
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulConnectSidecarsInsufficientToken(f *framework.F) {
	// create policy that can write services into apple, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
namespace "apple" {
  service "count-api" { policy = "write" }
  service "count-dashboard" { policy = "write" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// submit job with insufficient token and expect error
	err := tc.submitJob(f, tokenID, cnsJobConnectSidecars)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: insufficient Consul ACL permissions to write service`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulConnectSidecarsMissingToken(f *framework.F) {
	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobConnectSidecars)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulConnectIngressGatewayMinimalToken(f *framework.F) {
	// create policy that can write services into apple, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
service "my-ingress-service-z" { policy = "write" }
service "uuid-api-z" { policy = "write" }

namespace "apple" {
  service "my-ingress-service" { policy = "write" }
  service "uuid-api" { policy = "write" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// run test with limited token
	tc.testConsulConnectIngressGateway(f, tokenID, "apple", "default")
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulConnectIngressGatewayInsufficientToken(f *framework.F) {
	// create policy that can write services into apple, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
service "my-ingress-service-z" { policy = "write" }
service "uuid-api-z" { policy = "write" }

namespace "apple" {
  service "my-ingress-service" { policy = "write" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// submit job with insufficient token and expect error
	err := tc.submitJob(f, tokenID, cnsJobConnectIngress)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: insufficient Consul ACL permissions to write service`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulConnectIngressGatewayMissingToken(f *framework.F) {
	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobConnectIngress)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulConnectTerminatingGatewayMinimalToken(f *framework.F) {
	// create policy that can write services into apple, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
service "count-api-z" { policy = "write" }
service "api-gateway-z" { policy = "write" }
service "count-dashboard-z" { policy = "write" }

namespace "apple" {
  service "count-api" { policy = "write" }
  service "api-gateway" { policy = "write" }
  service "count-dashboard" { policy = "write" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// run test with limited token
	tc.testConsulConnectTerminatingGateway(f, tokenID, "apple", "default")
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulConnectTerminatingGatewayInsufficientToken(f *framework.F) {
	// create policy that can write services into apple, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
service "count-api-z" { policy = "write" }

namespace "apple" {
  service "api-gateway" { policy = "write" }
  service "count-dashboard" { policy = "write" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// submit job with insufficient token and expect error
	err := tc.submitJob(f, tokenID, cnsJobConnectTerminating)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: insufficient Consul ACL permissions to write service`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulConnectTerminatingGatewayMissingToken(f *framework.F) {
	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobConnectTerminating)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulScriptChecksTaskMinimalToken(f *framework.F) {
	// create policy that can write services into apple, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
service_prefix "service-" { policy = "write" }

namespace "apple" {
  service_prefix "service-" { policy = "write" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// run test with limited token
	tc.testConsulScriptChecksTask(f, tokenID, "apple", "default")
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulScriptChecksTaskInsufficientToken(f *framework.F) {
	// create policy that can write services into apple, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
service_prefix "service-" { policy = "write" }

namespace "apple" {
  service_prefix "something-" { policy = "write" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// submit job with insufficient token and expect error
	err := tc.submitJob(f, tokenID, cnsJobScriptChecksTask)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: insufficient Consul ACL permissions to write service`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulScriptChecksTaskMissingToken(f *framework.F) {
	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobScriptChecksTask)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulScriptChecksGroupMinimalToken(f *framework.F) {
	// create policy that can write services into apple, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
service_prefix "service-" { policy = "write" }

namespace "apple" {
  service_prefix "service-" { policy = "write" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// run test with limited token
	tc.testConsulScriptChecksGroup(f, tokenID, "apple", "default")
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulScriptChecksGroupInsufficientToken(f *framework.F) {
	// create policy that can write services into apple, default
	policyID := e2eutil.CreateConsulPolicy(f.T(), tc.Consul(), "default", e2eutil.ConsulPolicy{
		Name: "multi-namespace-" + uuid.Short(),
		Rules: `
service_prefix "service-" { policy = "write" }

namespace "other" {
  service_prefix "service-" { policy = "write" }
}
`,
	})
	tc.addPolicy("default", policyID)

	// create token from the policy
	tokenID := e2eutil.CreateConsulToken(f.T(), tc.Consul(), "default", policyID)
	tc.addToken("default", tokenID)

	// submit job with insufficient token and expect error
	err := tc.submitJob(f, tokenID, cnsJobScriptChecksGroup)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: insufficient Consul ACL permissions to write service`)
}

func (tc *ConsulNamespacesE2ETestACLs) TestConsulScriptChecksGroupMissingToken(f *framework.F) {
	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobScriptChecksGroup)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}
