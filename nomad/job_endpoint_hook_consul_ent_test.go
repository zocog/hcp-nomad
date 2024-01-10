//go:build ent
// +build ent

package nomad

import (
	"testing"

	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestJobEndpointHook_ConsulEnt(t *testing.T) {
	ci.Parallel(t)

	srv, cleanup := TestServer(t, func(c *Config) {
		c.NumSchedulers = 0
	})
	t.Cleanup(cleanup)
	testutil.WaitForLeader(t, srv.RPC)

	job := mock.Job()

	// create two group-level services and assign to clusters
	taskSvc := job.TaskGroups[0].Tasks[0].Services[0]
	taskSvc.Provider = structs.ServiceProviderConsul
	taskSvc.Cluster = "nondefault"
	job.TaskGroups[0].Tasks[0].Services = []*structs.Service{taskSvc}

	job.TaskGroups[0].Services = append(job.TaskGroups[0].Services, taskSvc.Copy())
	job.TaskGroups[0].Services = append(job.TaskGroups[0].Services, taskSvc.Copy())
	job.TaskGroups[0].Services[0].Cluster = ""
	job.TaskGroups[0].Services[1].Cluster = "infra"

	// assign to a specific partition
	job.TaskGroups[0].Consul = &structs.Consul{Partition: "foo"}

	// add a second group with the same tasks/services but different partition
	otherGroup := job.TaskGroups[0].Copy()
	otherGroup.Consul = &structs.Consul{Cluster: "infra", Partition: "bar"}
	job.TaskGroups = append(job.TaskGroups, otherGroup)

	cases := []struct {
		name         string
		cfg          *structs.NamespaceConsulConfiguration
		expectGroup0 string
		expectGroup1 string
		expectTask0  string

		expectError string
	}{
		{
			name:         "no consul config",
			expectGroup0: "default",
			expectGroup1: "infra",
			expectTask0:  "nondefault",
		},
		{
			name: "has allowed set",
			cfg: &structs.NamespaceConsulConfiguration{
				Default: "nondefault",
				Allowed: []string{"nondefault", "default", "infra"},
				Denied:  []string{},
			},
			expectGroup0: "nondefault",
			expectGroup1: "infra",
			expectTask0:  "nondefault",
		},
		{
			name: "not in allowed set",
			cfg: &structs.NamespaceConsulConfiguration{
				Default: "default",
				Allowed: []string{"nondefault"},
				Denied:  []string{},
			},
			expectGroup0: "default",
			expectGroup1: "infra",
			expectTask0:  "nondefault",
			expectError:  `namespace "default" does not allow jobs to use consul cluster "infra"`,
		},
		{
			name: "has denied set",
			cfg: &structs.NamespaceConsulConfiguration{
				Default: "default",
				Denied:  []string{"infra", "nondefault"},
			},
			expectGroup0: "default",
			expectGroup1: "infra",
			expectTask0:  "nondefault",
			expectError: `2 errors occurred:
	* namespace "default" does not allow jobs to use consul cluster "infra"
	* namespace "default" does not allow jobs to use consul cluster "nondefault"

`,
		},
		{
			name: "empty allowlist denies all",
			cfg: &structs.NamespaceConsulConfiguration{
				Default: "default",
				Allowed: []string{},
			},
			expectGroup0: "default",
			expectGroup1: "infra",
			expectTask0:  "nondefault",
			expectError: `2 errors occurred:
	* namespace "default" does not allow jobs to use consul cluster "infra"
	* namespace "default" does not allow jobs to use consul cluster "nondefault"

`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			job := job.Copy()

			ns := mock.Namespace()
			ns.Name = job.Namespace
			ns.ConsulConfiguration = tc.cfg
			ns.SetHash()
			srv.fsm.State().UpsertNamespaces(1000, []*structs.Namespace{ns})

			hook := jobConsulHook{srv}

			_, _, err := hook.Mutate(job)

			must.NoError(t, err)
			test.Eq(t, tc.expectGroup0, job.TaskGroups[0].Services[0].Cluster)
			test.Eq(t, tc.expectGroup1, job.TaskGroups[0].Services[1].Cluster)
			test.Eq(t, tc.expectTask0, job.TaskGroups[0].Tasks[0].Services[0].Cluster)

			test.SliceContains(t, job.TaskGroups[0].Constraints,
				&structs.Constraint{
					LTarget: "${attr.consul.partition}",
					RTarget: "foo",
					Operand: "=",
				})

			test.SliceContains(t, job.TaskGroups[1].Constraints,
				&structs.Constraint{
					LTarget: "${attr.consul.infra.partition}",
					RTarget: "bar",
					Operand: "=",
				})

			_, err = hook.Validate(job)
			if tc.expectError != "" {
				must.EqError(t, err, tc.expectError)
			} else {
				must.NoError(t, err)
			}
		})
	}

}
