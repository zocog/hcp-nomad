// +build ent

package nomad

import (
	"testing"
	"time"

	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
)

func TestLicenseEndpoint_GetLicense(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s1, cleanupS1 := TestServer(t, nil)
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	get := &structs.LicenseGetRequest{
		QueryOptions: structs.QueryOptions{Region: "global"},
	}

	var resp structs.LicenseGetResponse
	require.NoError(t, msgpackrpc.CallWithCodec(codec, "License.GetLicense", get, &resp))
	require.NotNil(t, resp.NomadLicense)

	got := resp.NomadLicense.License
	require.NotNil(t, got)
	require.Equal(t, "test-license", got.LicenseID)
	require.Equal(t, "test customer id", got.CustomerID)
	require.True(t, got.IssueTime.After(now.Add(-1*time.Minute)))
	require.True(t, got.StartTime.After(now.Add(-1*time.Minute)))
	require.True(t, got.ExpirationTime.After(now.Add(15*time.Minute)))
	require.True(t, got.TerminationTime.After(now.Add(15*time.Minute)))
	require.Empty(t, got.Flags)
}
