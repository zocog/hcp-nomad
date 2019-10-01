// +build pro ent

package client

import (
	"testing"

	cstructs "github.com/hashicorp/nomad/client/structs"
	"github.com/hashicorp/nomad/client/testutil"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/stretchr/testify/require"
)

// TestFS_List_Namespace asserts that a token with read-fs in namespace A
// cannot read files for allocations in namespace B.
func TestFS_List_Namespaces(t *testing.T) {
	t.Parallel()

	h := setupAllocNamespaces(t, nil)
	defer h.cleanup()

	cases := []struct {
		Name   string
		Req    *cstructs.FsListRequest
		Assert func(error) bool
	}{
		{
			Name: "allowed",
			Req: &cstructs.FsListRequest{
				AllocID: h.allocs[0],
				Path:    "/",
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: func(err error) bool { return err == nil },
		},
		{
			Name: "wrong-ns",
			Req: &cstructs.FsListRequest{
				AllocID: h.allocs[1],
				Path:    "/",
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: structs.IsErrPermissionDenied,
		},
		{
			Name: "disallowed",
			Req: &cstructs.FsListRequest{
				AllocID: h.allocs[1],
				Path:    "/",
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "prod",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: structs.IsErrPermissionDenied,
		},
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var resp cstructs.FsListResponse
			err := h.client.ClientRPC("FileSystem.List", c.Req, &resp)
			require.True(t, c.Assert(err), "(%T) %s", err, err)
		})
	}
}

// TestFS_Logs_Namespace asserts that a token with read-fs in namespace A
// cannot read files for allocations in namespace B.
func TestFS_Logs_Namespaces(t *testing.T) {
	t.Parallel()

	h := setupAllocNamespaces(t, nil)
	defer h.cleanup()

	cases := []testutil.StreamingRPCErrorTestCase{
		{
			Name: "allowed",
			RPC:  "FileSystem.Logs",
			Req: &cstructs.FsLogsRequest{
				AllocID: h.allocs[0],
				Task:    h.jobs[0].TaskGroups[0].Tasks[0].Name,
				LogType: "stdout",
				Origin:  "start",
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: func(err error) bool { return err == nil },
		},
		{
			Name: "wrong-ns",
			RPC:  "FileSystem.Logs",
			Req: &cstructs.FsLogsRequest{
				AllocID: h.allocs[1],
				Task:    h.jobs[1].TaskGroups[0].Tasks[0].Name,
				LogType: "stdout",
				Origin:  "start",
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: structs.IsErrPermissionDenied,
		},
		{
			Name: "disallowed",
			RPC:  "FileSystem.Logs",
			Req: &cstructs.FsLogsRequest{
				AllocID: h.allocs[1],
				Task:    h.jobs[1].TaskGroups[0].Tasks[0].Name,
				LogType: "stdout",
				Origin:  "start",
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "prod",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: structs.IsErrPermissionDenied,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			testutil.AssertStreamingRPCError(t, h.client, tc)
		})
	}
}

// TestFS_Stat_Namespace asserts that a token with read-fs in namespace A
// cannot read files for allocations in namespace B.
func TestFS_Stat_Namespaces(t *testing.T) {
	t.Parallel()

	h := setupAllocNamespaces(t, nil)
	defer h.cleanup()

	cases := []struct {
		Name   string
		Req    *cstructs.FsStatRequest
		Assert func(error) bool
	}{
		{
			Name: "allowed",
			Req: &cstructs.FsStatRequest{
				AllocID: h.allocs[0],
				Path:    "/",
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: func(err error) bool { return err == nil },
		},
		{
			Name: "wrong-ns",
			Req: &cstructs.FsStatRequest{
				AllocID: h.allocs[1],
				Path:    "/",
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: structs.IsErrPermissionDenied,
		},
		{
			Name: "disallowed",
			Req: &cstructs.FsStatRequest{
				AllocID: h.allocs[1],
				Path:    "/",
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "prod",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: structs.IsErrPermissionDenied,
		},
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var resp cstructs.FsStatResponse
			err := h.client.ClientRPC("FileSystem.Stat", c.Req, &resp)
			require.True(t, c.Assert(err), "(%T) %s", err, err)
		})
	}
}

// TestFS_Stream_Namespace asserts that a token with read-fs in namespace A
// cannot stream files for allocations in namespace B.
func TestFS_Stream_Namespaces(t *testing.T) {
	t.Parallel()

	h := setupAllocNamespaces(t, nil)
	defer h.cleanup()

	cases := []testutil.StreamingRPCErrorTestCase{
		{
			Name: "allowed",
			RPC:  "FileSystem.Stream",
			Req: &cstructs.FsStreamRequest{
				AllocID:   h.allocs[0],
				Path:      "alloc/logs/web.stdout.0",
				PlainText: true,
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: func(err error) bool { return err == nil },
		},
		{
			Name: "wrong-ns",
			RPC:  "FileSystem.Stream",
			Req: &cstructs.FsStreamRequest{
				AllocID:   h.allocs[1],
				Path:      "alloc/logs/web.stdout.0",
				PlainText: true,
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "staging",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: structs.IsErrPermissionDenied,
		},
		{
			Name: "disallowed",
			RPC:  "FileSystem.Stream",
			Req: &cstructs.FsStreamRequest{
				AllocID:   h.allocs[1],
				Path:      "alloc/logs/web.stdout.0",
				PlainText: true,
				QueryOptions: structs.QueryOptions{
					Region:    "global",
					Namespace: "prod",
					AuthToken: h.token.SecretID,
				},
			},
			Assert: structs.IsErrPermissionDenied,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			testutil.AssertStreamingRPCError(t, h.client, tc)
		})
	}
}
