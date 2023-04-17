//go:build ent
// +build ent

package nomad

import (
	"testing"
	"time"

	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc/v2"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/shoenig/test/must"
)

func TestLicenseEndpoint_GetLicense(t *testing.T) {
	ci.Parallel(t)
	now := time.Now()
	s1, cleanupS1 := TestServer(t, nil)
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	get := &structs.LicenseGetRequest{
		QueryOptions: structs.QueryOptions{Region: "global"},
	}

	var resp structs.LicenseGetResponse
	must.NoError(t, msgpackrpc.CallWithCodec(codec, "License.GetLicense", get, &resp))
	must.NotNil(t, resp.NomadLicense)

	got := resp.NomadLicense.License
	must.NotNil(t, got)
	must.Eq(t, "test-license", got.LicenseID)
	must.Eq(t, "test customer id", got.CustomerID)
	must.True(t, got.IssueTime.After(now.Add(-1*time.Minute)))
	must.True(t, got.StartTime.After(now.Add(-1*time.Minute)))
	must.True(t, got.ExpirationTime.After(now.Add(15*time.Minute)))
	must.True(t, got.TerminationTime.After(now.Add(15*time.Minute)))
	must.MapEmpty(t, got.Flags)
}
