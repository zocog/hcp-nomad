//go:build ent
// +build ent

package taskrunner

import (
	"testing"

	regMock "github.com/hashicorp/nomad/client/serviceregistration/mock"
	"github.com/hashicorp/nomad/client/serviceregistration/wrapper"
	cstructs "github.com/hashicorp/nomad/client/structs"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/shoenig/test/must"
)

func Test_serviceHook_Namespaces(t *testing.T) {
	alloc := mock.Alloc()

	alloc.Job.TaskGroups[0].Consul = &structs.Consul{Namespace: "nondefault1"}

	task := alloc.LookupTask("web")
	task2 := task.Copy()
	task2.Name = "foo"
	task2.Consul = &structs.Consul{Namespace: "nondefault2"}
	alloc.Job.TaskGroups[0].Tasks = append(alloc.Job.TaskGroups[0].Tasks, task2)

	alloc.Job.Canonicalize()

	logger := testlog.HCLogger(t)
	c := regMock.NewServiceRegistrationHandler(logger)
	regWrap := wrapper.NewHandlerWrapper(logger, c, nil)

	hook2 := newServiceHook(serviceHookConfig{
		alloc:             alloc,
		task:              alloc.LookupTask("foo"),
		serviceRegWrapper: regWrap,
		logger:            logger,
		hookResources:     cstructs.NewAllocHookResources(),
	})
	must.Eq(t, "nondefault2", hook2.providerNamespace)

	hook1 := newServiceHook(serviceHookConfig{
		alloc:             alloc,
		task:              alloc.LookupTask("web"),
		serviceRegWrapper: regWrap,
		logger:            logger,
		hookResources:     cstructs.NewAllocHookResources(),
	})
	must.Eq(t, "nondefault1", hook1.providerNamespace)
}
