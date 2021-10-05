//go:build ent
// +build ent

package nomad

import (
	"testing"

	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/nomad/acl"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
)

func TestDeploymentEndpoint_Get_CrossNamespace(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	s1, _, cleanupS1 := TestACLServer(t, func(c *Config) {
		c.NumSchedulers = 0 // Prevent automatic dequeue
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// two namespaces, two write policies
	ns1 := mock.Namespace()
	ns2 := mock.Namespace()
	state := s1.fsm.State()
	require.NoError(state.UpsertNamespaces(1000, []*structs.Namespace{ns1, ns2}))

	// Create the policies nd tokens
	token1 := mock.CreatePolicyAndToken(t, state, 1001, "policy1",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilityReadJob}))
	token2 := mock.CreatePolicyAndToken(t, state, 1002, "policy2",
		mock.NamespacePolicy(ns2.Name, "", []string{acl.NamespaceCapabilityReadJob}))

	// Create deployment in ns1
	depl := mock.Deployment()
	depl.Namespace = ns1.Name
	require.NoError(state.UpsertDeployment(1003, depl))

	// Access deployment from ns1, mostly making sure everything is set up correctly
	var resp structs.SingleDeploymentResponse
	req := &structs.DeploymentSpecificRequest{
		DeploymentID: depl.ID,
		QueryOptions: structs.QueryOptions{
			Region:    "global",
			Namespace: ns1.Name,
			AuthToken: token1.SecretID,
		},
	}
	err := msgpackrpc.CallWithCodec(codec, "Deployment.GetDeployment", req, &resp)
	require.NoError(err)
	require.NotNil(resp.Deployment)

	// Access using credentials for ns2; should 404 because our token doesn't have read-job on ns1
	req.Namespace = ns2.Name
	req.AuthToken = token2.SecretID
	err = msgpackrpc.CallWithCodec(codec, "Deployment.GetDeployment", req, &resp)
	require.NoError(err)
	require.Nil(resp.Deployment)
}
