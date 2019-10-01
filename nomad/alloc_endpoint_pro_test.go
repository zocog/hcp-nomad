// +build pro ent

package nomad

import (
	"net/rpc"
	"testing"

	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	"github.com/hashicorp/nomad/acl"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/require"
)

// setupAllocNamespaces creates a server, token, and 2 allocs in 2 namespaces.
func setupAllocNamespaces(t *testing.T) ([]*structs.Allocation, rpc.ClientCodec, string, *Server) {
	s1, _ := TestACLServer(t, nil)
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Create the alloc
	alloc1 := mock.Alloc()
	alloc1.Namespace = "staging"
	alloc2 := mock.Alloc()
	alloc2.Namespace = "prod"
	allocs := []*structs.Allocation{alloc1, alloc2}
	state := s1.fsm.State()

	ns1 := mock.Namespace()
	ns1.Name = "staging"
	ns2 := mock.Namespace()
	ns2.Name = "prod"
	require.Nil(t, state.UpsertNamespaces(100, []*structs.Namespace{ns1}))
	require.Nil(t, state.UpsertNamespaces(200, []*structs.Namespace{ns2}))

	require.NoError(t, state.UpsertJobSummary(998, mock.JobSummary(alloc1.JobID)))
	require.NoError(t, state.UpsertJobSummary(999, mock.JobSummary(alloc2.JobID)))
	require.NoError(t, state.UpsertAllocs(1000, allocs))

	// Create a token with read-job in namespace "staging" but *not*
	// namespace "prod"
	validToken := mock.CreatePolicyAndToken(t, state, 1001, "staging-only",
		mock.NamespacePolicy("staging", "", []string{acl.NamespaceCapabilityListJobs,
			acl.NamespaceCapabilityReadJob, acl.NamespaceCapabilitySubmitJob,
			acl.NamespaceCapabilityDispatchJob, acl.NamespaceCapabilityReadLogs,
			acl.NamespaceCapabilityReadFS, acl.NamespaceCapabilityAllocLifecycle,
			acl.NamespaceCapabilityAllocExec, acl.NamespaceCapabilityAllocNodeExec}))

	return allocs, codec, validToken.SecretID, s1
}

// TestAllocEndpoint_GetAlloc_Namespaces asserts that a token with read-job in
// namespace A cannot read allocs in namespace B.
func TestAllocEndpoint_GetAlloc_Namespaces(t *testing.T) {
	t.Parallel()
	allocs, codec, token, s := setupAllocNamespaces(t)
	defer s.Shutdown()

	req := &structs.AllocSpecificRequest{
		AllocID: allocs[0].ID,
		QueryOptions: structs.QueryOptions{
			AuthToken: token,
			Region:    "global",
			Namespace: "staging",
		},
	}

	// staging alloc should be gettable
	var resp structs.SingleAllocResponse
	require.NoError(t, msgpackrpc.CallWithCodec(codec, "Alloc.GetAlloc", req, &resp))

	// prod alloc should 404
	req.AllocID = allocs[1].ID
	err := msgpackrpc.CallWithCodec(codec, "Alloc.GetAlloc", req, &resp)
	require.True(t, structs.IsErrUnknownAllocation(err))

	// when namespace is properly set the error is permission denied
	req.QueryOptions.Namespace = "prod"
	err = msgpackrpc.CallWithCodec(codec, "Alloc.GetAlloc", req, &resp)
	require.EqualError(t, err, structs.ErrPermissionDenied.Error())
}

// TestAllocEndpoint_Stop_Namespaces asserts that a token with alloc-lifecycle
// in namespace A cannot read allocs in namespace B.
func TestAllocEndpoint_Stop_Namespaces(t *testing.T) {
	t.Parallel()
	allocs, codec, token, s := setupAllocNamespaces(t)
	defer s.Shutdown()

	req := &structs.AllocStopRequest{
		AllocID: allocs[0].ID,
		WriteRequest: structs.WriteRequest{
			AuthToken: token,
			Region:    "global",
			Namespace: "staging",
		},
	}

	// staging alloc should be gettable
	var resp structs.AllocStopResponse
	require.NoError(t, msgpackrpc.CallWithCodec(codec, "Alloc.Stop", req, &resp))

	// prod alloc should 404
	req.AllocID = allocs[1].ID
	err := msgpackrpc.CallWithCodec(codec, "Alloc.Stop", req, &resp)
	require.EqualError(t, err, structs.ErrPermissionDenied.Error())

	// when namespace is properly set the error is permission denied
	req.WriteRequest.Namespace = "prod"
	err = msgpackrpc.CallWithCodec(codec, "Alloc.Stop", req, &resp)
	require.EqualError(t, err, structs.ErrPermissionDenied.Error())
}
