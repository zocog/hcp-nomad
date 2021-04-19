// +build ent

package consul

import (
	"github.com/hashicorp/nomad/e2e/framework"
)

func (tc *ConsulNamespacesE2ETest) TestConsulRegisterGroupService(f *framework.F) {
	tc.testConsulRegisterGroupServices(f, "apple", "banana", "cherry", "default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulRegisterTaskServices(f *framework.F) {
	tc.testConsulRegisterTaskServices(f, "apple", "banana", "cherry", "default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulTemplateKV(f *framework.F) {
	tc.testConsulTemplateKV(f, "value: ns_banana", "value: ns_default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulConnectSidecars(f *framework.F) {
	tc.testConsulConnectSidecars(f, "apple", "default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulConnectIngressGateway(f *framework.F) {
	tc.testConsulConnectIngressGateway(f, "apple", "default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulConnectTerminatingGateway(f *framework.F) {
	tc.testConsulConnectTerminatingGateway(f, "apple", "default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulScriptChecksTask(f *framework.F) {
	tc.testConsulScriptChecksTask(f, "apple", "default")
}

func (tc *ConsulNamespacesE2ETest) TestConsulScriptChecksGroup(f *framework.F) {
	tc.testConsulScriptChecksGroup(f, "apple", "default")
}
