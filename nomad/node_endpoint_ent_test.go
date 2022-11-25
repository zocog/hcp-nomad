//go:build ent
// +build ent

package nomad

import (
	"fmt"
	"testing"
	"time"

	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/client"
	"github.com/hashicorp/nomad/client/config"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/require"
)

// TestClientEndpoint_Drain_Multiregion asserts that a multiregion job gets the
// expected placements when a node drains and is marked as eligible again.
func TestClientEndpoint_Drain_Multiregion(t *testing.T) {
	ci.Parallel(t)

	// Create a server in a client in the same region. Even though this is a
	// multiregion test we only need to act in a single region.
	s, cleanupS := TestServer(t, func(conf *Config) {
		conf.Region = "east"
		conf.LicenseEnv = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS()
	codec := rpcClient(t, s)

	c, cleanupC := client.TestClient(t, func(conf *config.Config) {
		conf.Region = "east"
		conf.Node.Datacenter = "east-1"
		conf.Servers = []string{s.GetConfig().RPCAddr.String()}
	})
	defer cleanupC()

	testutil.WaitForLeader(t, s.RPC)

	// Create a multiregion job with some parameters tightened to speed up
	// testing.
	var jobResp structs.JobRegisterResponse
	job := mock.MultiregionJob()
	job.Constraints = []*structs.Constraint{}
	job.Update = structs.UpdateStrategy{
		HealthCheck:      structs.UpdateStrategyHealthCheck_TaskStates,
		MinHealthyTime:   time.Duration(100*testutil.TestMultiplier()) * time.Millisecond,
		HealthyDeadline:  time.Duration(30*testutil.TestMultiplier()) * time.Second,
		ProgressDeadline: time.Duration(testutil.TestMultiplier()) * time.Minute,
	}
	job.Multiregion = &structs.Multiregion{
		Strategy: &structs.MultiregionStrategy{
			MaxParallel: 1,
			OnFailure:   "fail_all",
		},
		Regions: []*structs.MultiregionRegion{
			{
				Name:        "east",
				Count:       1,
				Datacenters: []string{"east-1"},
				Meta:        map[string]string{"region_code": "E"},
			},
		},
	}
	job.TaskGroups[0].Count = 0
	job.TaskGroups[0].Constraints = []*structs.Constraint{}
	job.TaskGroups[0].Tasks[0].Driver = "mock_driver"
	job.TaskGroups[0].Tasks[0].Config = map[string]interface{}{
		"run_for": "300s",
	}
	job.TaskGroups[0].Tasks[0].Services = []*structs.Service{}

	// Register the multiregion job.
	jobReq := &structs.JobRegisterRequest{
		Job:          job,
		WriteRequest: structs.WriteRequest{Region: "east"},
	}
	err := msgpackrpc.CallWithCodec(codec, "Job.Register", jobReq, &jobResp)
	require.NoError(t, err)

	// Wait for deployment to complete.
	testutil.WaitForResultUntil(time.Duration(testutil.TestMultiplier())*time.Minute, func() (bool, error) {
		req := &structs.JobSpecificRequest{
			JobID:        job.ID,
			QueryOptions: structs.QueryOptions{Region: "east"},
		}
		var resp structs.SingleDeploymentResponse

		err := msgpackrpc.CallWithCodec(codec, "Job.LatestDeployment", req, &resp)
		if err != nil {
			return false, err
		}

		if resp.Deployment.Status == structs.DeploymentStatusSuccessful {
			return true, nil
		}

		return false, fmt.Errorf("status: %s", resp.Deployment.Status)
	}, func(err error) {
		t.Fatal(err)
	})

	// Drain node.
	strategy := &structs.DrainStrategy{
		DrainSpec: structs.DrainSpec{
			Deadline: time.Second,
		},
	}
	drainReq := &structs.NodeUpdateDrainRequest{
		NodeID:        c.NodeID(),
		DrainStrategy: strategy,
		WriteRequest:  structs.WriteRequest{Region: "east"},
	}
	var drainResp structs.NodeDrainUpdateResponse
	err = msgpackrpc.CallWithCodec(codec, "Node.UpdateDrain", drainReq, &drainResp)
	require.NoError(t, err)

	// Wait for allocation to be complete.
	testutil.WaitForResultUntil(time.Duration(testutil.TestMultiplier())*time.Minute, func() (bool, error) {
		req := &structs.JobSpecificRequest{
			JobID:        job.ID,
			QueryOptions: structs.QueryOptions{Region: "east"},
		}
		var resp structs.JobAllocationsResponse

		err := msgpackrpc.CallWithCodec(codec, "Job.Allocations", req, &resp)
		if err != nil {
			return false, err
		}

		for _, alloc := range resp.Allocations {
			if alloc.ClientStatus != structs.AllocClientStatusComplete {
				return false, fmt.Errorf("status: %s", alloc.ClientStatus)
			}
		}

		return true, nil
	}, func(err error) {
		t.Fatal(err)
	})

	// Mark node as eligible again.
	drainReq.DrainStrategy = nil
	drainReq.MarkEligible = true
	err = msgpackrpc.CallWithCodec(codec, "Node.UpdateDrain", drainReq, &drainResp)
	require.NoError(t, err)
	require.Len(t, drainResp.EvalIDs, 1)

	// Wait for allocation to be running.
	testutil.WaitForResultUntil(time.Duration(testutil.TestMultiplier())*time.Minute, func() (bool, error) {
		req := &structs.JobSpecificRequest{
			JobID:        job.ID,
			QueryOptions: structs.QueryOptions{Region: "east"},
		}
		var resp structs.JobAllocationsResponse

		err := msgpackrpc.CallWithCodec(codec, "Job.Allocations", req, &resp)
		if err != nil {
			return false, err
		}

		for _, alloc := range resp.Allocations {
			if alloc.ClientStatus == structs.AllocClientStatusRunning {
				return true, nil
			}
		}

		return false, fmt.Errorf("expected at least one alloc to be running")
	}, func(err error) {
		t.Fatal(err)
	})
}
