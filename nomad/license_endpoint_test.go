package nomad

import (
	"testing"

	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/assert"
)

func TestLicenseEndpoint_GetLicense(t *testing.T) {
	assert := assert.New(t)
	t.Parallel()
	s1, cleanupS1 := TestServer(t, nil)
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	l := mock.StoredLicense()
	s1.fsm.State().UpsertLicense(1000, l)

	get := &structs.LicenseGetRequest{
		QueryOptions: structs.QueryOptions{Region: "global"},
	}
	var resp structs.LicenseGetResponse
	assert.Nil(msgpackrpc.CallWithCodec(codec, "License.GetLicense", get, &resp))
	assert.EqualValues(1000, resp.Index)
	assert.Equal(l, resp.License)
}

func TestLicenseEndpoint_UpsertLicense(t *testing.T) {
	assert := assert.New(t)
	t.Parallel()
	s1, cleanupS1 := TestServer(t, nil)
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Create the register request
	l := mock.StoredLicense()

	req := &structs.LicenseUpsertRequest{
		License:      l,
		WriteRequest: structs.WriteRequest{Region: "global"},
	}
	var resp structs.GenericResponse
	assert.Nil(msgpackrpc.CallWithCodec(codec, "License.UpsertLicense", req, &resp))
	assert.NotEqual(uint64(0), resp.Index)

	// Check we created the license
	out, err := s1.fsm.State().License(nil)
	assert.Equal(out.Signed, l.Signed)
	assert.Nil(err)
	assert.NotNil(out)
}
