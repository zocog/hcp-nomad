// +build pro ent

package client

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/nomad/acl"
	"github.com/hashicorp/nomad/client/config"
	cstructs "github.com/hashicorp/nomad/client/structs"
	ctestutil "github.com/hashicorp/nomad/client/testutil"
	"github.com/hashicorp/nomad/nomad"
	"github.com/hashicorp/nomad/nomad/mock"
	nstructs "github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/require"
)

type namespaceTestHelper struct {
	server          *nomad.Server
	client          *Client
	clientCleanupFn func() error

	jobs   []*nstructs.Job
	allocs []string

	token *nstructs.ACLToken
}

func (n *namespaceTestHelper) cleanup() {
	n.clientCleanupFn()
	n.server.Shutdown()
}

func setupAllocNamespaces(t *testing.T, jobCustomizer func(*nstructs.Job)) *namespaceTestHelper {
	t.Helper()

	helper := &namespaceTestHelper{}

	// Setup a Server
	s1, addr, rootToken := testACLServer(t, nil)
	testutil.WaitForLeader(t, s1.RPC)
	helper.server = s1

	state := s1.State()
	ns1 := mock.Namespace()
	ns1.Name = "staging"
	ns2 := mock.Namespace()
	ns2.Name = "prod"
	require.NoError(t, state.UpsertNamespaces(100, []*nstructs.Namespace{ns1}))
	require.NoError(t, state.UpsertNamespaces(200, []*nstructs.Namespace{ns2}))

	client, cleanup := TestClient(t, func(c *config.Config) {
		c.Servers = []string{addr}
		//c.RPCHandler = s1
		c.ACLEnabled = true
	})
	helper.client = client
	helper.clientCleanupFn = cleanup

	// Create the jobs
	job1 := mock.Job()
	job1.TaskGroups[0].Count = 1
	job1.TaskGroups[0].Tasks[0].Driver = "mock_driver"
	job1.TaskGroups[0].Tasks[0].Config = map[string]interface{}{
		"run_for":       "20s",
		"stdout_string": "thank you, next",
	}
	job1.Namespace = "staging"
	if jobCustomizer != nil {
		jobCustomizer(job1)
	}

	job2 := mock.Job()
	job2.TaskGroups[0].Count = 1
	job2.TaskGroups[0].Tasks[0].Driver = "mock_driver"
	job2.TaskGroups[0].Tasks[0].Config = map[string]interface{}{
		"run_for":       "20s",
		"stdout_string": "thank you, next",
	}
	job2.Namespace = "prod"
	if jobCustomizer != nil {
		jobCustomizer(job2)
	}

	jobs := []*nstructs.Job{job1, job2}
	helper.jobs = jobs

	// Register them on the server and wait for them to run

	allocList1 := testutil.WaitForRunningWithToken(t, s1.RPC, job1, rootToken.SecretID)
	allocList2 := testutil.WaitForRunningWithToken(t, s1.RPC, job2, rootToken.SecretID)

	helper.allocs = append(helper.allocs, allocList1[0].ID)
	helper.allocs = append(helper.allocs, allocList2[0].ID)

	// Create a token with read-job in namespace "staging" but *not*
	// namespace "prod"
	validToken := mock.CreatePolicyAndToken(t, state, 1000, "staging-only",
		mock.NamespacePolicy("staging", "", []string{acl.NamespaceCapabilityListJobs,
			acl.NamespaceCapabilityReadJob, acl.NamespaceCapabilitySubmitJob,
			acl.NamespaceCapabilityDispatchJob, acl.NamespaceCapabilityReadLogs,
			acl.NamespaceCapabilityReadFS, acl.NamespaceCapabilityAllocLifecycle,
			acl.NamespaceCapabilityAllocExec, acl.NamespaceCapabilityAllocNodeExec}))
	helper.token = validToken

	return helper
}

func TestAllocations_Restart_Namespaces(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	th := setupAllocNamespaces(t, nil)
	defer th.cleanup()

	// Staging Alloc should be restartable
	{
		req := &nstructs.AllocRestartRequest{
			AllocID: th.allocs[0],
			QueryOptions: nstructs.QueryOptions{
				AuthToken: th.token.SecretID,
				Region:    "global",
				Namespace: "staging",
			},
		}
		var resp nstructs.GenericResponse
		err := th.client.ClientRPC("Allocations.Restart", &req, &resp)
		require.NoError(err)
	}
	// Prod Alloc should not be restartable
	{
		req := &nstructs.AllocRestartRequest{
			AllocID: th.allocs[1],
			QueryOptions: nstructs.QueryOptions{
				AuthToken: th.token.SecretID,
				Region:    "global",
				Namespace: "prod",
			},
		}
		var resp nstructs.GenericResponse
		err := th.client.ClientRPC("Allocations.Restart", &req, &resp)
		require.Error(err)
	}
	// Prod Alloc should not be restartable with a namespace bypass
	{
		req := &nstructs.AllocRestartRequest{
			AllocID: th.allocs[1],
			QueryOptions: nstructs.QueryOptions{
				AuthToken: th.token.SecretID,
				Region:    "global",
				Namespace: "staging",
			},
		}
		var resp nstructs.GenericResponse
		err := th.client.ClientRPC("Allocations.Restart", &req, &resp)
		require.Error(err)
	}
}

func TestAllocations_Signal_Namespaces(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	th := setupAllocNamespaces(t, nil)
	defer th.cleanup()

	// Staging Alloc should be signalable
	{
		req := &nstructs.AllocSignalRequest{
			AllocID: th.allocs[0],
			Signal:  "SIGKILL",
			QueryOptions: nstructs.QueryOptions{
				AuthToken: th.token.SecretID,
				Region:    "global",
				Namespace: "staging",
			},
		}
		var resp nstructs.GenericResponse
		err := th.client.ClientRPC("Allocations.Signal", &req, &resp)
		require.NoError(err)
	}
	// Prod Alloc should not be signallable
	{
		req := &nstructs.AllocSignalRequest{
			AllocID: th.allocs[1],
			Signal:  "SIGKILL",
			QueryOptions: nstructs.QueryOptions{
				AuthToken: th.token.SecretID,
				Region:    "global",
				Namespace: "prod",
			},
		}
		var resp nstructs.GenericResponse
		err := th.client.ClientRPC("Allocations.Signal", &req, &resp)
		require.True(nstructs.IsErrPermissionDenied(err))
	}
	// Prod Alloc should not be restartable with a namespace bypass
	{
		req := &nstructs.AllocSignalRequest{
			AllocID: th.allocs[1],
			Signal:  "SIGKILL",
			QueryOptions: nstructs.QueryOptions{
				AuthToken: th.token.SecretID,
				Region:    "global",
				Namespace: "staging",
			},
		}
		var resp nstructs.GenericResponse
		err := th.client.ClientRPC("Allocations.Signal", &req, &resp)
		require.True(nstructs.IsErrPermissionDenied(err), "(%T) %v", err, err)
	}
}

func TestAllocations_Stats_Namespaces(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	th := setupAllocNamespaces(t, nil)
	defer th.cleanup()

	// Staging Alloc should work
	{
		req := &cstructs.AllocStatsRequest{
			AllocID: th.allocs[0],
			QueryOptions: nstructs.QueryOptions{
				AuthToken: th.token.SecretID,
				Region:    "global",
				Namespace: "staging",
			},
		}
		var resp cstructs.AllocStatsResponse
		err := th.client.ClientRPC("Allocations.Stats", &req, &resp)
		require.NoError(err)
	}
	// Prod Alloc should not work
	{
		req := &cstructs.AllocStatsRequest{
			AllocID: th.allocs[1],
			QueryOptions: nstructs.QueryOptions{
				AuthToken: th.token.SecretID,
				Region:    "global",
				Namespace: "prod",
			},
		}
		var resp cstructs.AllocStatsResponse
		err := th.client.ClientRPC("Allocations.Stats", &req, &resp)
		require.True(nstructs.IsErrPermissionDenied(err))
	}
	// Prod Alloc should not work with a namespace bypass
	{
		req := &cstructs.AllocStatsRequest{
			AllocID: th.allocs[1],
			QueryOptions: nstructs.QueryOptions{
				AuthToken: th.token.SecretID,
				Region:    "global",
				Namespace: "staging",
			},
		}
		var resp cstructs.AllocStatsResponse
		err := th.client.ClientRPC("Allocations.Stats", &req, &resp)
		require.True(nstructs.IsErrPermissionDenied(err))
	}
}

func TestAllocations_GarbageCollect_Namespaces(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name             string
		allocIdx         int
		requestNamespace string
		errChecker       func(error) (bool, error)
	}{
		{
			name:             "with matching ns/allocns",
			allocIdx:         0,
			requestNamespace: "staging",
			errChecker: func(err error) (bool, error) {
				if err == nil {
					return true, nil
				} else {
					return false, fmt.Errorf("No error was expected, got: %v", err)
				}
			},
		},
		{
			name:             "with ns you don't have permission to access",
			allocIdx:         1,
			requestNamespace: "prod",
			errChecker: func(err error) (bool, error) {
				if err == nil {
					return false, fmt.Errorf("Expected error, got none")
				} else if nstructs.IsErrPermissionDenied(err) {
					return true, nil
				} else {
					return false, fmt.Errorf("Unexpected error, expected permission err: %v", err)
				}
			},
		},
		{
			name:             "with mismatched ns/allocns",
			allocIdx:         1,
			requestNamespace: "staging",
			errChecker: func(err error) (bool, error) {
				if err == nil {
					return false, fmt.Errorf("Expected error, got none")
				} else if nstructs.IsErrPermissionDenied(err) {
					return true, nil
				} else {
					return false, fmt.Errorf("Unexpected error, expected unknown alloc err: %v", err)
				}
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			require := require.New(t)
			th := setupAllocNamespaces(t, func(j *nstructs.Job) {
				j.TaskGroups[0].Tasks[0].Config = map[string]interface{}{
					"run_for":       "50ms",
					"stdout_string": "thank you, next",
				}
				j.TaskGroups[0].RestartPolicy = &nstructs.RestartPolicy{
					Attempts: 0,
					Interval: 10 * time.Second,
					Mode:     nstructs.RestartPolicyModeFail,
				}
			})
			defer th.cleanup()

			allocID := th.allocs[tc.allocIdx]

			testutil.WaitForResult(func() (bool, error) {
				// Check it has been removed first
				if ar, ok := th.client.allocs[allocID]; !ok || ar.IsDestroyed() {
					return true, nil
				}

				req := &nstructs.AllocSpecificRequest{
					AllocID: allocID,
					QueryOptions: nstructs.QueryOptions{
						AuthToken: th.token.SecretID,
						Region:    "global",
						Namespace: tc.requestNamespace,
					},
				}
				var resp nstructs.GenericResponse
				err := th.client.ClientRPC("Allocations.GarbageCollect", &req, &resp)
				return tc.errChecker(err)
			}, func(err error) {
				require.NoError(err)
			})
		})
	}
}

// TestAllocations_exec_Namespace asserts that a token with alloc-exec in
// namespace A cannot exec into allocations in namespace B.
func TestAllocations_exec_Namespace(t *testing.T) {
	t.Parallel()

	h := setupAllocNamespaces(t, nil)
	defer h.cleanup()

	cases := []ctestutil.StreamingRPCErrorTestCase{
		{
			Name: "allowed",
			RPC:  "Allocations.Exec",
			Req: &cstructs.AllocExecRequest{
				AllocID: h.allocs[0],
				Task:    h.jobs[0].TaskGroups[0].Tasks[0].Name,
				Cmd:     []string{"ls"},
				QueryOptions: nstructs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: func(err error) bool {
				return strings.Contains(err.Error(), "no exec command")
			},
		},
		{
			Name: "wrong-ns",
			RPC:  "Allocations.Exec",
			Req: &cstructs.AllocExecRequest{
				AllocID: h.allocs[1],
				Task:    h.jobs[1].TaskGroups[0].Tasks[0].Name,
				Cmd:     []string{"ls"},
				QueryOptions: nstructs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: nstructs.IsErrPermissionDenied,
		},
		{
			Name: "disallowed",
			RPC:  "Allocations.Exec",
			Req: &cstructs.AllocExecRequest{
				AllocID: h.allocs[1],
				Task:    h.jobs[1].TaskGroups[0].Tasks[0].Name,
				Cmd:     []string{"ls"},
				QueryOptions: nstructs.QueryOptions{
					Region:    "global",
					Namespace: "prod",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: nstructs.IsErrPermissionDenied,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			ctestutil.AssertStreamingRPCError(t, h.client, tc)
		})
	}
}
