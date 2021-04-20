// +build ent

package consul

import (
	"os"

	"github.com/hashicorp/nomad/e2e/framework"
)

func init() {
	// Consul Namespace tests with Consul ACLs enabled. Like Connect w/ ACLs tests,
	// these are gated behind the NOMAD_TEST_CONSUL_ACLS environment variable,
	// because they cause lots of problems for e2e test flakiness (due to restarting
	// consul, nomad, etc.).
	//
	// Run these tests locally when working on Consul.
	if os.Getenv("NOMAD_TEST_CONSUL_ACLS") == "1" {
		framework.AddSuites(&framework.TestSuite{
			Component:   "ConsulACLs",
			CanRunLocal: false,
			Consul:      true,
			Parallel:    false,
			Cases: []framework.TestCase{
				new(ConsulNamespacesE2ETestACLs),
			},
		})
	}
}

// In these tests, tc.cToken is empty when Consul ACLs are not enabled, and is
// set to the global-management token when ACLs are enabled. In both cases tests
// should work, as the management token has complete access to everything.

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
