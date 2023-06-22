//go:build ent
// +build ent

package nomad

import (
	"fmt"
	"net/rpc"
	"testing"

	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/shoenig/test/must"
)

func TestNodePoolEndpoint_License(t *testing.T) {
	ci.Parallel(t)

	// Create a server with a license with the right features.
	sLicensed, cleanupSLicensed := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForGovernance().Signed
	})
	defer cleanupSLicensed()

	// Create a server with a license without features.
	sUnlicensed, cleanupSUnlicensed := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = defaultTestLicense()
	})
	defer cleanupSUnlicensed()

	testCases := []struct {
		name        string
		endpoint    string
		codec       rpc.ClientCodec
		req         any
		expectedErr string
	}{
		{
			name:     "licensed/list allowed",
			endpoint: "List",
			codec:    rpcClient(t, sLicensed),
			req: &structs.NodePoolListRequest{
				QueryOptions: structs.QueryOptions{Region: "global"},
			},
		},
		{
			name:     "licensed/read allowed",
			endpoint: "GetNodePool",
			codec:    rpcClient(t, sLicensed),
			req: &structs.NodePoolSpecificRequest{
				Name:         "default",
				QueryOptions: structs.QueryOptions{Region: "global"},
			},
		},
		{
			name:     "licensed/upsert allowed without scheduler config",
			endpoint: "UpsertNodePools",
			codec:    rpcClient(t, sLicensed),
			req: &structs.NodePoolUpsertRequest{
				NodePools: []*structs.NodePool{
					{Name: "new"},
				},
				WriteRequest: structs.WriteRequest{Region: "global"},
			},
		},
		{
			name:     "licensed/upsert allowed with scheduler config",
			endpoint: "UpsertNodePools",
			codec:    rpcClient(t, sLicensed),
			req: &structs.NodePoolUpsertRequest{
				NodePools: []*structs.NodePool{
					{
						Name: "new",
						SchedulerConfiguration: &structs.NodePoolSchedulerConfiguration{
							SchedulerAlgorithm: structs.SchedulerAlgorithmBinpack,
						},
					},
				},
				WriteRequest: structs.WriteRequest{Region: "global"},
			},
		},
		{
			name:     "licensed/delete allowed",
			endpoint: "DeleteNodePools",
			codec:    rpcClient(t, sLicensed),
			req: &structs.NodePoolDeleteRequest{
				Names:        []string{"existing"},
				WriteRequest: structs.WriteRequest{Region: "global"},
			},
		},
		{
			name:     "unlicensed/list allowed",
			endpoint: "List",
			codec:    rpcClient(t, sUnlicensed),
			req: &structs.NodePoolListRequest{
				QueryOptions: structs.QueryOptions{Region: "global"},
			},
		},
		{
			name:     "unlicensed/read allowed",
			endpoint: "GetNodePool",
			codec:    rpcClient(t, sUnlicensed),
			req: &structs.NodePoolSpecificRequest{
				Name:         "default",
				QueryOptions: structs.QueryOptions{Region: "global"},
			},
		},
		{
			name:     "unlicensed/upsert allowed without scheduler config",
			endpoint: "UpsertNodePools",
			codec:    rpcClient(t, sUnlicensed),
			req: &structs.NodePoolUpsertRequest{
				NodePools: []*structs.NodePool{
					{Name: "new"},
				},
				WriteRequest: structs.WriteRequest{Region: "global"},
			},
		},
		{
			name:     "unlicensed/upsert denied with scheduler config",
			endpoint: "UpsertNodePools",
			codec:    rpcClient(t, sUnlicensed),
			req: &structs.NodePoolUpsertRequest{
				NodePools: []*structs.NodePool{
					{
						Name: "new",
						SchedulerConfiguration: &structs.NodePoolSchedulerConfiguration{
							SchedulerAlgorithm: structs.SchedulerAlgorithmBinpack,
						},
					},
				},
				WriteRequest: structs.WriteRequest{Region: "global"},
			},
			expectedErr: "unlicensed",
		},
		{
			name:     "unlicensed/delete allowed",
			endpoint: "DeleteNodePools",
			codec:    rpcClient(t, sUnlicensed),
			req: &structs.NodePoolDeleteRequest{
				Names:        []string{"existing"},
				WriteRequest: structs.WriteRequest{Region: "global"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Make sure node pool to be deleted exists.
			existingPool := &structs.NodePool{Name: "existing"}
			sLicensed.fsm.State().UpsertNodePools(structs.MsgTypeTestSetup, 1000, []*structs.NodePool{
				existingPool,
			})
			sUnlicensed.fsm.State().UpsertNodePools(structs.MsgTypeTestSetup, 1000, []*structs.NodePool{
				existingPool,
			})

			var resp any
			err := msgpackrpc.CallWithCodec(tc.codec, fmt.Sprintf("NodePool.%s", tc.endpoint), tc.req, resp)
			if tc.expectedErr != "" {
				must.ErrorContains(t, err, tc.expectedErr)
			} else {
				must.NoError(t, err)
			}
		})
	}
}
