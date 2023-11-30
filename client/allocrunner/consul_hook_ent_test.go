//go:build ent
// +build ent

package allocrunner

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"testing"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/client/consul"
	cstructs "github.com/hashicorp/nomad/client/structs"
	"github.com/hashicorp/nomad/client/widmgr"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	structsc "github.com/hashicorp/nomad/nomad/structs/config"
	"github.com/shoenig/test/must"
)

func consulHookEntTestHarness(t *testing.T) *consulHook {
	logger := testlog.HCLogger(t)

	alloc := mock.Alloc()
	task := alloc.LookupTask("web")
	task.Consul = &structs.Consul{
		Cluster: "foo",
	}
	task.Identities = []*structs.WorkloadIdentity{
		{Name: fmt.Sprintf("%s_foo", structs.ConsulTaskIdentityNamePrefix)},
	}
	task.Services = []*structs.Service{
		{
			Provider: structs.ServiceProviderConsul,
			Identity: &structs.WorkloadIdentity{Name: "consul-service_webservice", Audience: []string{"consul.io"}},
			Cluster:  "foo",
			Name:     "webservice",
			TaskName: "web",
		},
	}

	identitiesToSign := []*structs.WorkloadIdentity{}
	identitiesToSign = append(identitiesToSign, task.Identities...)
	for _, service := range task.Services {
		identitiesToSign = append(identitiesToSign, service.Identity)
	}

	// setup mock signer and sign the identities
	mockSigner := widmgr.NewMockWIDSigner(identitiesToSign)
	signedIDs, err := mockSigner.SignIdentities(1, []*structs.WorkloadIdentityRequest{
		{
			AllocID: alloc.ID,
			WIHandle: structs.WIHandle{
				WorkloadIdentifier: task.Name,
				IdentityName:       task.Identities[0].Name,
			},
		},
		{
			AllocID: alloc.ID,
			WIHandle: structs.WIHandle{
				WorkloadIdentifier: task.Services[0].Name,
				IdentityName:       task.Services[0].Identity.Name,
				WorkloadType:       structs.WorkloadTypeService,
			},
		},
	})
	must.NoError(t, err)

	mockWIDMgr := widmgr.NewMockWIDMgr(signedIDs)

	consulConfigs := map[string]*structsc.ConsulConfig{
		"foo": {Name: "foo"},
	}

	hookResources := cstructs.NewAllocHookResources()

	consulHookCfg := consulHookConfig{
		alloc:                   alloc,
		allocdir:                nil,
		widmgr:                  mockWIDMgr,
		consulConfigs:           consulConfigs,
		consulClientConstructor: consul.NewMockConsulClient,
		hookResources:           hookResources,
		logger:                  logger,
	}
	return newConsulHook(consulHookCfg)
}

func Test_consulHook_nonDefaultCluster(t *testing.T) {
	ci.Parallel(t)

	tokens := map[string]map[string]*consulapi.ACLToken{}

	hook := consulHookEntTestHarness(t)
	task := hook.alloc.LookupTask("web")

	wid := task.GetIdentity("consul_foo")
	ti := *task.IdentityHandle(wid)
	taskJWT, err := hook.widmgr.Get(ti)
	must.NoError(t, err)

	hashTaskJWT := md5.Sum([]byte(taskJWT.JWT))

	hashServicesJWT := make(map[string]string)
	for _, s := range task.Services {
		widHandle := *s.IdentityHandle()
		jwt, err := hook.widmgr.Get(widHandle)
		must.NoError(t, err)

		hash := md5.Sum([]byte(jwt.JWT))
		hashServicesJWT[s.Name] = hex.EncodeToString(hash[:])
	}

	must.NoError(t, hook.prepareConsulTokensForTask(task, nil, tokens))
	must.NoError(t, hook.prepareConsulTokensForServices(task.Services, nil, tokens))
	must.Eq(t, map[string]map[string]*consulapi.ACLToken{
		"foo": {
			"consul-service_webservice": &consulapi.ACLToken{
				AccessorID: hashServicesJWT["webservice"],
				SecretID:   hashServicesJWT["webservice"],
			},
			"consul_foo": &consulapi.ACLToken{
				AccessorID: hex.EncodeToString(hashTaskJWT[:]),
				SecretID:   hex.EncodeToString(hashTaskJWT[:]),
			},
		}}, tokens)
}
