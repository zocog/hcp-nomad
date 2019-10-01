// +build pro ent

package nomad

import (
	"testing"

	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	cstructs "github.com/hashicorp/nomad/client/structs"
	"github.com/hashicorp/nomad/client/testutil"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/stretchr/testify/require"
)

// TestclientFS_List_Namespaces asserts that a token with read-fs in namespace
// A cannot list files for allocs in namespace B.
func TestClientFS_List_Namespaces(t *testing.T) {
	t.Parallel()

	allocs, codec, token, s := setupAllocNamespaces(t)
	defer s.Shutdown()

	// Make the request
	req := &cstructs.FsListRequest{
		AllocID: allocs[0].ID,
		Path:    "/",
		QueryOptions: structs.QueryOptions{
			Region:    "global",
			Namespace: "staging",
			AuthToken: token,
		},
	}

	// Since there's no client to respond, even the valid namespace will
	// return an error.
	var resp cstructs.FsListResponse
	err := msgpackrpc.CallWithCodec(codec, "FileSystem.List", req, &resp)
	require.True(t, structs.IsErrUnknownNode(err))

	// Requests against the prod namespace should fail before attempting to
	// lookup the node without explicitly setting the proper namespace.
	req.AllocID = allocs[1].ID
	err = msgpackrpc.CallWithCodec(codec, "FileSystem.List", req, &resp)
	require.True(t, structs.IsErrPermissionDenied(err), "(%T) %s", err, err)

	// Requests against the prod namespace should fail before attempting to
	// lookup the node when explicitly setting the proper namespace.
	req.Namespace = allocs[1].Namespace
	err = msgpackrpc.CallWithCodec(codec, "FileSystem.List", req, &resp)
	require.True(t, structs.IsErrPermissionDenied(err), "(%T) %s", err, err)
}

// TestClientFS_Logs_Namespaces asserts that a token with read-logs in
// namespace A cannot read logs for allocs in namespace B.
func TestClientFS_Logs_Namespaces(t *testing.T) {
	t.Parallel()

	allocs, _, token, s := setupAllocNamespaces(t)
	defer s.Shutdown()

	cases := []testutil.StreamingRPCErrorTestCase{
		{
			Name: "allowed",
			RPC:  "FileSystem.Logs",
			Req: &cstructs.FsLogsRequest{
				AllocID: allocs[0].ID,
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: token,
				},
			},
			Assert: structs.IsErrUnknownNode,
		},
		{
			Name: "fail-wrong-ns",
			RPC:  "FileSystem.Logs",
			Req: &cstructs.FsLogsRequest{
				AllocID: allocs[1].ID,
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: token,
				},
			},
			Assert: structs.IsErrPermissionDenied,
		},
		{
			Name: "disallowed",
			RPC:  "FileSystem.Logs",
			Req: &cstructs.FsLogsRequest{
				AllocID: allocs[1].ID,
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "prod",
					AuthToken: token,
				},
			},
			Assert: structs.IsErrPermissionDenied,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			testutil.AssertStreamingRPCError(t, s, tc)
		})
	}
}

// TestClientFS_Streaming_Namespaces asserts that a token with read-fs in
// namespace A cannot read files for allocs in namespace B.
func TestClientFS_Streaming_Namespaces(t *testing.T) {
	t.Parallel()

	allocs, _, token, s := setupAllocNamespaces(t)
	defer s.Shutdown()

	cases := []testutil.StreamingRPCErrorTestCase{
		{
			Name: "allowed",
			RPC:  "FileSystem.Stream",
			Req: &cstructs.FsStreamRequest{
				AllocID: allocs[0].ID,
				Path:    "/foo",
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: token,
				},
			},
			Assert: structs.IsErrUnknownNode,
		},
		{
			Name: "fail-wrong-ns",
			RPC:  "FileSystem.Stream",
			Req: &cstructs.FsStreamRequest{
				AllocID: allocs[1].ID,
				Path:    "/foo",
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: token,
				},
			},
			Assert: structs.IsErrPermissionDenied,
		},
		{
			Name: "disallowed",
			RPC:  "FileSystem.Stream",
			Req: &cstructs.FsStreamRequest{
				AllocID: allocs[1].ID,
				Path:    "/foo",
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "prod",
					AuthToken: token,
				},
			},
			Assert: structs.IsErrPermissionDenied,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			testutil.AssertStreamingRPCError(t, s, tc)
		})
	}
}
