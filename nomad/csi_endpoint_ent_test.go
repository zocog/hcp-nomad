// +build ent

package nomad

import (
	"testing"

	memdb "github.com/hashicorp/go-memdb"
	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	"github.com/hashicorp/nomad/acl"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/require"
)

// TestCSIPluginEndpoint_ACLNamespaceAlloc checks that allocations are filtered by namespace
// when getting plugins, and enforcing that the client has job-read ACL access to the
// namespace of the allocations
func TestCSIPluginEndpoint_ACLNamespaceAlloc(t *testing.T) {
	t.Parallel()
	srv, shutdown := TestServer(t, func(c *Config) {
		c.NumSchedulers = 0 // Prevent automatic dequeue
	})
	defer shutdown()
	testutil.WaitForLeader(t, srv.RPC)
	s := srv.fsm.State()

	ns1 := mock.Namespace()
	require.NoError(t, s.UpsertNamespaces(1000, []*structs.Namespace{ns1}))

	// Setup ACLs
	s.BootstrapACLTokens(structs.MsgTypeTestSetup, 1, 0, mock.ACLManagementToken())
	srv.config.ACLEnabled = true
	codec := rpcClient(t, srv)
	listJob := mock.NamespacePolicy(structs.DefaultNamespace, "", []string{acl.NamespaceCapabilityReadJob})
	policy := mock.PluginPolicy("read") + listJob
	getToken := mock.CreatePolicyAndToken(t, s, 1001, "plugin-read", policy)

	// Create the plugin and then some allocations to pretend to be the allocs that are
	// running the plugin tasks
	deleteNodes := state.CreateTestCSIPlugin(srv.fsm.State(), "foo")
	defer deleteNodes()

	plug, _ := s.CSIPluginByID(memdb.NewWatchSet(), "foo")
	var allocs []*structs.Allocation
	for _, info := range plug.Controllers {
		a := mock.Alloc()
		a.ID = info.AllocID
		allocs = append(allocs, a)
	}
	for _, info := range plug.Nodes {
		a := mock.Alloc()
		a.ID = info.AllocID
		allocs = append(allocs, a)
	}

	require.Equal(t, 3, len(allocs))
	allocs[0].Namespace = ns1.Name

	err := s.UpsertAllocs(structs.MsgTypeTestSetup, 1003, allocs)
	require.NoError(t, err)

	req := &structs.CSIPluginGetRequest{
		ID: "foo",
		QueryOptions: structs.QueryOptions{
			Region:    "global",
			AuthToken: getToken.SecretID,
		},
	}
	resp := &structs.CSIPluginGetResponse{}
	err = msgpackrpc.CallWithCodec(codec, "CSIPlugin.Get", req, resp)
	require.NoError(t, err)
	require.Equal(t, 2, len(resp.Plugin.Allocations))

	for _, a := range resp.Plugin.Allocations {
		require.Equal(t, structs.DefaultNamespace, a.Namespace)
	}

	p2 := mock.PluginPolicy("read")
	t2 := mock.CreatePolicyAndToken(t, s, 1004, "plugin-read2", p2)
	req.AuthToken = t2.SecretID
	err = msgpackrpc.CallWithCodec(codec, "CSIPlugin.Get", req, resp)
	require.NoError(t, err)
	require.Equal(t, 0, len(resp.Plugin.Allocations))
}
