//go:build ent
// +build ent

package consul

import (
	"os"

	"github.com/hashicorp/nomad/e2e/e2eutil"
	"github.com/hashicorp/nomad/e2e/framework"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/stretchr/testify/require"
)

func (tc *ConsulNamespacesE2ETest) TestConsulRegisterGroupService(f *framework.F) {
	tc.testConsulRegisterGroupServices(f, tc.cToken, "apple", "banana", "cherry", "default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulRegisterTaskServices(f *framework.F) {
	tc.testConsulRegisterTaskServices(f, tc.cToken, "apple", "banana", "cherry", "default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulTemplateKV(f *framework.F) {
	tc.testConsulTemplateKV(f, tc.cToken, "value: ns_banana", "value: ns_default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulConnectSidecars(f *framework.F) {
	tc.testConsulConnectSidecars(f, tc.cToken, "apple", "default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulConnectIngressGateway(f *framework.F) {
	tc.testConsulConnectIngressGateway(f, tc.cToken, "apple", "default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulConnectTerminatingGateway(f *framework.F) {
	tc.testConsulConnectTerminatingGateway(f, tc.cToken, "apple", "default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulScriptChecksTask(f *framework.F) {
	tc.testConsulScriptChecksTask(f, tc.cToken, "apple", "default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulScriptChecksGroup(f *framework.F) {
	tc.testConsulScriptChecksGroup(f, tc.cToken, "apple", "default")
}

func (tc *ConsulNamespacesE2ETest) addPolicy(namespace, policyID string) {
	tc.policyIDs[namespace] = append(tc.policyIDs[namespace], policyID)
}

func (tc *ConsulNamespacesE2ETest) addToken(namespace, tokenID string) {
	tc.tokenIDs[namespace] = append(tc.tokenIDs[namespace], tokenID)
}

func (tc *ConsulNamespacesE2ETest) AfterEach(f *framework.F) {
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
	tc.jobIDs = []string{}
	tc.tokenIDs = make(map[string][]string)
	tc.policyIDs = make(map[string][]string)
}

// submitJob is used in tests that expect job submission to fail, due to the
// use of Consul tokens with insufficient or missing ACL policies. The error
// returned is coming from job.Register, and should be validated in the test.
func (tc *ConsulNamespacesE2ETest) submitJob(f *framework.F, token, file string) error {
	job, parseErr := e2eutil.Parse2(f.T(), file)
	f.NoError(parseErr)

	if token != "" {
		job.ConsulToken = &token
	}

	jobAPI := tc.Nomad().Jobs()
	_, _, err := jobAPI.Register(job, nil)
	tc.jobIDs = append(tc.jobIDs, *job.ID)
	return err
}

func (tc *ConsulNamespacesE2ETest) TestConsulRegisterGroupServiceMinimalToken(f *framework.F) {
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

func (tc *ConsulNamespacesE2ETest) TestConsulRegisterGroupServiceInsufficientToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

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

func (tc *ConsulNamespacesE2ETest) TestConsulRegisterGroupServiceMissingToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobGroupServices)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETest) TestConsulRegisterTaskServicesMinimalToken(f *framework.F) {
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

func (tc *ConsulNamespacesE2ETest) TestConsulRegisterTaskServicesInsufficientToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

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

func (tc *ConsulNamespacesE2ETest) TestConsulRegisterTaskServicesMissingToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobTaskServices)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETest) TestConsulTemplateKVMinimalToken(f *framework.F) {
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

func (tc *ConsulNamespacesE2ETest) TestConsulTemplateKVInsufficientToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

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

func (tc *ConsulNamespacesE2ETest) TestConsulTemplateKVMissingToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

	// submit job with without token and expect error
	err := tc.submitJob(f, "", cnsJobTemplateKV)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETest) TestConsulConnectSidecarsMinimalToken(f *framework.F) {
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

func (tc *ConsulNamespacesE2ETest) TestConsulConnectSidecarsInsufficientToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

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

func (tc *ConsulNamespacesE2ETest) TestConsulConnectSidecarsMissingToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobConnectSidecars)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETest) TestConsulConnectIngressGatewayMinimalToken(f *framework.F) {
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

func (tc *ConsulNamespacesE2ETest) TestConsulConnectIngressGatewayInsufficientToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

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

func (tc *ConsulNamespacesE2ETest) TestConsulConnectIngressGatewayMissingToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobConnectIngress)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETest) TestConsulConnectTerminatingGatewayMinimalToken(f *framework.F) {
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

func (tc *ConsulNamespacesE2ETest) TestConsulConnectTerminatingGatewayInsufficientToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

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

func (tc *ConsulNamespacesE2ETest) TestConsulConnectTerminatingGatewayMissingToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobConnectTerminating)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETest) TestConsulScriptChecksTaskMinimalToken(f *framework.F) {
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

func (tc *ConsulNamespacesE2ETest) TestConsulScriptChecksTaskInsufficientToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

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

func (tc *ConsulNamespacesE2ETest) TestConsulScriptChecksTaskMissingToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobScriptChecksTask)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}

func (tc *ConsulNamespacesE2ETest) TestConsulScriptChecksGroupMinimalToken(f *framework.F) {
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

func (tc *ConsulNamespacesE2ETest) TestConsulScriptChecksGroupInsufficientToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

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

func (tc *ConsulNamespacesE2ETest) TestConsulScriptChecksGroupMissingToken(f *framework.F) {

	f.T().Skip("we don't have consul.allow_unauthenticated=false set because it would required updating every E2E test to pass a Consul token")

	// submit job without token and expect error
	err := tc.submitJob(f, "", cnsJobScriptChecksGroup)
	require.Error(f.T(), err)
	require.Contains(f.T(), err.Error(), `job-submitter consul token denied: missing consul token`)
}
