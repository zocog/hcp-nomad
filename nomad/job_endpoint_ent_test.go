//go:build ent
// +build ent

package nomad

import (
	"context"
	"fmt"
	"net/rpc"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-memdb"
	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/command/agent/consul"
	"github.com/hashicorp/nomad/helper/pointer"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/shoenig/test/must"
	"github.com/shoenig/test/wait"
)

func TestJobEndpoint_Register_Sentinel(t *testing.T) {
	ci.Parallel(t)
	s1, root, cleanupS1 := TestACLServer(t, func(c *Config) {
		c.NumSchedulers = 0 // Prevent automatic dequeue
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Create a passing policy
	policy1 := mock.SentinelPolicy()
	policy1.EnforcementLevel = structs.SentinelEnforcementLevelHardMandatory
	s1.State().UpsertSentinelPolicies(1000,
		[]*structs.SentinelPolicy{policy1})

	// Create the register request
	job := mock.Job()
	req := &structs.JobRegisterRequest{
		Job: job,
		WriteRequest: structs.WriteRequest{
			Region:    "global",
			AuthToken: root.SecretID,
		},
	}

	// Should work
	var resp structs.JobRegisterResponse
	if err := msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp); err != nil {
		t.Fatalf("err: %v", err)
	}

	// Create a failing policy
	policy2 := mock.SentinelPolicy()
	policy2.EnforcementLevel = structs.SentinelEnforcementLevelSoftMandatory
	policy2.Policy = "main = rule { false }"
	s1.State().UpsertSentinelPolicies(1001,
		[]*structs.SentinelPolicy{policy2})

	// Should fail
	if err := msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp); err == nil {
		t.Fatalf("expected error")
	}

	// Do the same request with an override set
	req.PolicyOverride = true

	// Should work, with a warning
	if err := msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(resp.Warnings, policy2.Name) {
		t.Fatalf("bad: %s", resp.Warnings)
	}
}

func TestJobEndpoint_Register_Sentinel_DriverForce(t *testing.T) {
	ci.Parallel(t)
	s1, root, cleanupS1 := TestACLServer(t, func(c *Config) {
		c.NumSchedulers = 0 // Prevent automatic dequeue
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Create a passing policy
	policy1 := mock.SentinelPolicy()
	policy1.EnforcementLevel = structs.SentinelEnforcementLevelHardMandatory
	policy1.Policy = `
	main = rule { all_drivers_exec }

	all_drivers_exec = rule {
		all job.task_groups as tg {
			all tg.tasks as task {
				task.driver is "exec"
			}
		}
	}
	`
	s1.State().UpsertSentinelPolicies(1000,
		[]*structs.SentinelPolicy{policy1})

	// Create the register request
	job := mock.Job()
	req := &structs.JobRegisterRequest{
		Job: job,
		WriteRequest: structs.WriteRequest{
			Region:    "global",
			AuthToken: root.SecretID,
		},
	}

	// Should work
	var resp structs.JobRegisterResponse
	if err := msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp); err != nil {
		t.Fatalf("err: %v", err)
	}

	// Create a failing job
	job2 := mock.Job()
	job2.TaskGroups[0].Tasks[0].Driver = "docker"
	req.Job = job2

	// Should fail
	if err := msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp); err == nil {
		t.Fatalf("expected error")
	}

	// Should fail even with override
	req.PolicyOverride = true
	if err := msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp); err == nil {
		t.Fatalf("expected error")
	}
}

func TestJobEndpoint_Register_Sentinel_Token_And_Namespace(t *testing.T) {
	ci.Parallel(t)
	s1, root, cleanupS1 := TestACLServer(t, func(c *Config) {
		c.NumSchedulers = 0 // Prevent automatic dequeue

	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	nsAndTokenPolicy := mock.SentinelPolicy()
	nsAndTokenPolicy.EnforcementLevel = structs.SentinelEnforcementLevelHardMandatory
	nsAndTokenPolicy.Policy = combinedPolicy
	s1.State().UpsertSentinelPolicies(1000,
		[]*structs.SentinelPolicy{nsAndTokenPolicy})

	validToken := root.Copy()
	validToken.Policies = []string{"foo"}

	invalidToken := root.Copy()
	invalidToken.Policies = []string{"baz"}

	validNS := mock.Namespace()
	validNS.Meta["userlist"] = "[bob]"

	invalidNS := mock.Namespace()
	invalidNS.Meta["userlist"] = "[suzy]"

	testCases := []struct {
		name        string
		token       *structs.ACLToken
		namespace   *structs.Namespace
		expectError string
	}{
		{
			name:        "allowed request",
			token:       validToken,
			namespace:   validNS,
			expectError: "",
		},
		{
			name:        "token not allowed",
			token:       invalidToken,
			namespace:   validNS,
			expectError: "does not have the correct policy",
		},
		{
			name:        "user not allowed for namespace",
			token:       validToken,
			namespace:   invalidNS,
			expectError: "not permitted on namespace",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := s1.State().UpsertACLTokens(structs.MsgTypeTestSetup, 100, []*structs.ACLToken{tc.token})
			must.NoError(t, err, must.Sprintf("failed to upsert ACL token: %v", err))

			err = s1.State().UpsertNamespaces(125, []*structs.Namespace{tc.namespace})
			must.NoError(t, err, must.Sprintf("failed to upsert namespace"))

			job := mock.Job()
			job.Namespace = tc.namespace.Name
			job.NomadTokenID = tc.token.SecretID
			for _, tg := range job.TaskGroups {
				for _, t := range tg.Tasks {
					t.User = "bob"
				}
			}
			req := &structs.JobRegisterRequest{
				Job: job,
				WriteRequest: structs.WriteRequest{
					Region:    "global",
					AuthToken: root.SecretID,
					Namespace: tc.namespace.Name,
				},
			}

			var resp structs.JobRegisterResponse
			err = msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp)
			if tc.expectError == "" {
				must.NoError(t, err, must.Sprint("failed to register job"))
			} else {
				must.Error(t, err, must.Sprint("should have errored"))
				must.StrContains(t, err.Error(), tc.expectError)

				// Should fail even with override because policy level is mandatory
				req.PolicyOverride = true
				err = msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp)
				must.Error(t, err, must.Sprint("should have errored with override"))
				must.StrContains(t, err.Error(), tc.expectError)
			}
		})
	}
}

func TestJobEndpoint_Plan_Sentinel(t *testing.T) {
	ci.Parallel(t)
	s1, root, cleanupS1 := TestACLServer(t, func(c *Config) {
		c.NumSchedulers = 0 // Prevent automatic dequeue
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Create a passing policy
	policy1 := mock.SentinelPolicy()
	policy1.EnforcementLevel = structs.SentinelEnforcementLevelHardMandatory
	policy1.Policy = `
	main = rule { all_drivers_exec }

	all_drivers_exec = rule {
		all job.task_groups as tg {
			all tg.tasks as task {
				task.driver is "exec"
			}
		}
	}
	`
	s1.State().UpsertSentinelPolicies(1000,
		[]*structs.SentinelPolicy{policy1})

	// Create a plan request
	job := mock.Job()
	planReq := &structs.JobPlanRequest{
		Job:  job,
		Diff: true,
		WriteRequest: structs.WriteRequest{
			Region:    "global",
			Namespace: job.Namespace,
			AuthToken: root.SecretID,
		},
	}

	// Fetch the response
	var planResp structs.JobPlanResponse
	if err := msgpackrpc.CallWithCodec(codec, "Job.Plan", planReq, &planResp); err != nil {
		t.Fatalf("err: %v", err)
	}

	// Create a failing job
	job2 := mock.Job()
	job2.TaskGroups[0].Tasks[0].Driver = "docker"
	planReq.Job = job2

	// Should fail
	if err := msgpackrpc.CallWithCodec(codec, "Job.Plan", planReq, &planResp); err == nil {
		t.Fatalf("expected error")
	}

	// Should fail, even with override
	planReq.PolicyOverride = true
	if err := msgpackrpc.CallWithCodec(codec, "Job.Plan", planReq, &planResp); err == nil {
		t.Fatalf("expected error")
	}
}

func TestJobEndpoint_Register_NodePool(t *testing.T) {
	ci.Parallel(t)

	s, cleanupS := TestServer(t, func(c *Config) {
		c.NumSchedulers = 0
	})
	defer cleanupS()
	codec := rpcClient(t, s)
	testutil.WaitForLeader(t, s.RPC)

	// Create test namespaces.
	nsWithDefault := mock.Namespace()
	nsWithDefault.NodePoolConfiguration.Default = "dev"

	nsAllowDev := mock.Namespace()
	nsAllowDev.NodePoolConfiguration.Allowed = []string{"dev*"}

	nsAllowNone := mock.Namespace()
	nsAllowNone.NodePoolConfiguration.Allowed = []string{}

	nsDenyDev := mock.Namespace()
	nsDenyDev.NodePoolConfiguration.Denied = []string{"dev*"}

	nsDenyDevWithDefault := mock.Namespace()
	nsDenyDevWithDefault.NodePoolConfiguration.Default = "dev"
	nsDenyDevWithDefault.NodePoolConfiguration.Denied = []string{"dev*"}

	nsReq := &structs.NamespaceUpsertRequest{
		Namespaces: []*structs.Namespace{
			nsWithDefault,
			nsAllowDev,
			nsAllowNone,
			nsDenyDev,
			nsDenyDevWithDefault,
		},
		WriteRequest: structs.WriteRequest{Region: "global"},
	}
	var nsResp structs.GenericResponse
	err := msgpackrpc.CallWithCodec(codec, "Namespace.UpsertNamespaces", nsReq, &nsResp)
	must.NoError(t, err)

	// Create test node pools.
	dev1Pool := mock.NodePool()
	dev1Pool.Name = "dev"

	dev2Pool := mock.NodePool()
	dev2Pool.Name = "dev2"

	prodPool := mock.NodePool()
	prodPool.Name = "prod"

	poolReq := &structs.NodePoolUpsertRequest{
		NodePools: []*structs.NodePool{
			dev1Pool,
			dev2Pool,
			prodPool,
		},
		WriteRequest: structs.WriteRequest{Region: "global"},
	}
	var poolResp structs.GenericResponse
	err = msgpackrpc.CallWithCodec(codec, "NodePool.UpsertNodePools", poolReq, &poolResp)
	must.NoError(t, err)

	testCases := []struct {
		name         string
		namespace    string
		nodePool     string
		expectedPool string
		expectedErr  string
	}{
		{
			name:         "job in default namespace uses default node pool",
			namespace:    structs.DefaultNamespace,
			nodePool:     "",
			expectedPool: structs.NodePoolDefault,
		},
		{
			name:         "job without node pool uses namespace default",
			namespace:    nsWithDefault.Name,
			nodePool:     "",
			expectedPool: nsWithDefault.NodePoolConfiguration.Default,
		},
		{
			name:         "job can override namespace default node pool",
			namespace:    nsWithDefault.Name,
			nodePool:     "prod",
			expectedPool: "prod",
		},
		{
			name:        "namespace can deny job from using node pool",
			namespace:   nsDenyDev.Name,
			nodePool:    "dev",
			expectedErr: "does not allow jobs to use node pool",
		},
		{
			name:         "namespace allows node pool unless denied",
			namespace:    nsDenyDev.Name,
			nodePool:     "prod",
			expectedPool: "prod",
		},
		{
			name:         "namespace can allow only specific node pools",
			namespace:    nsAllowDev.Name,
			nodePool:     "dev",
			expectedPool: "dev",
		},
		{
			name:        "namespace denies node pool unless allowed",
			namespace:   nsAllowDev.Name,
			nodePool:    "prod",
			expectedErr: "does not allow jobs to use node pool",
		},
		{
			name:        "namespace can deny all node pools",
			namespace:   nsAllowNone.Name,
			nodePool:    "dev2",
			expectedErr: "does not allow jobs to use node pool",
		},
		{
			name:        "namespace denies with glob",
			namespace:   nsDenyDev.Name,
			nodePool:    "dev2",
			expectedErr: "does not allow jobs to use node pool",
		},
		{
			name:         "namespace allows with glob",
			namespace:    nsAllowDev.Name,
			nodePool:     "dev2",
			expectedPool: "dev2",
		},
		{
			name:         "namespace allows if default pool matches deny glob",
			namespace:    nsDenyDevWithDefault.Name,
			nodePool:     "dev",
			expectedPool: "dev",
		},
		{
			name:         "namespace allows default pool even if not explicitly allowed",
			namespace:    nsAllowDev.Name,
			nodePool:     "default",
			expectedPool: "default",
		},
		{
			name:         "namespace allows default pool even if none are allowed",
			namespace:    nsAllowNone.Name,
			nodePool:     "default",
			expectedPool: "default",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := mock.Job()
			job.Namespace = tc.namespace
			job.NodePool = tc.nodePool

			req := &structs.JobRegisterRequest{
				Job: job,
				WriteRequest: structs.WriteRequest{
					Region:    "global",
					Namespace: job.Namespace,
				},
			}
			var resp structs.JobRegisterResponse
			err = msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp)

			if tc.expectedErr != "" {
				must.ErrorContains(t, err, tc.expectedErr)
			} else {
				must.NoError(t, err)

				got, err := s.State().JobByID(nil, job.Namespace, job.ID)
				must.NoError(t, err)
				must.Eq(t, tc.expectedPool, got.NodePool)
			}
		})
	}
}

func TestJobEndpoint_Register_Multiregion(t *testing.T) {
	ci.Parallel(t)

	west, root, cleanupWest := TestACLServer(t, func(c *Config) {
		c.Region = "west"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.NumSchedulers = 1
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupWest()

	east, _, cleanupEast := TestACLServer(t, func(c *Config) {
		c.Region = "east"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.ReplicationBackoff = 20 * time.Millisecond
		c.ReplicationToken = root.SecretID
		c.NumSchedulers = 1
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupEast()

	// this server will never receive workloads
	north, _, cleanupNorth := TestACLServer(t, func(c *Config) {
		c.Region = "north"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.ReplicationBackoff = 20 * time.Millisecond
		c.ReplicationToken = root.SecretID
		c.NumSchedulers = 1
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupNorth()

	TestJoin(t, west, east, north)
	testutil.WaitForLeader(t, west.RPC)
	testutil.WaitForLeader(t, east.RPC)
	testutil.WaitForLeader(t, north.RPC)

	// we'll pass all RPCs through the inactive region to ensure forwarding
	// is working as expected
	codec := rpcClient(t, north)

	// can't use Server.numPeers here b/c these are different regions
	testutil.WaitForResult(func() (bool, error) {
		return west.serf.NumNodes() == 3, nil
	}, func(err error) {
		t.Fatalf("should have 3 peers")
	})

	job := mock.MultiregionJob()
	req := &structs.JobRegisterRequest{
		Job: job,
		WriteRequest: structs.WriteRequest{
			Region:    "east",
			AuthToken: root.SecretID,
		},
	}

	var resp structs.JobRegisterResponse
	err := msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp)
	must.NoError(t, err)

	getReq := &structs.JobSpecificRequest{
		JobID: job.ID,
		QueryOptions: structs.QueryOptions{
			Region:    "east",
			AuthToken: root.SecretID,
		},
	}
	var getResp structs.SingleJobResponse
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	must.NoError(t, err)

	eastJob := getResp.Job
	must.NotNil(t, eastJob, must.Sprintf("getResp: %#v", getResp))
	must.Eq(t, "east", eastJob.Region)
	must.Eq(t, []string{"east-1"}, eastJob.Datacenters)
	must.Eq(t, "E", eastJob.Meta["region_code"])
	must.Eq(t, 10, eastJob.TaskGroups[0].Count)
	must.Eq(t, 0, eastJob.Version)

	getReq.Region = "west"
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	must.NoError(t, err)
	westJob := getResp.Job

	must.NotNil(t, westJob, must.Sprintf("getResp: %#v", getResp))
	must.Eq(t, "west", westJob.Region)
	must.Eq(t, []string{"west-1", "west-2"}, westJob.Datacenters)
	must.Eq(t, "W", westJob.Meta["region_code"])
	must.Eq(t, 10, westJob.TaskGroups[0].Count)
	must.Eq(t, westJob.Version, eastJob.Version)
	oldVersion := westJob.Version

	getReq.Region = "north"
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	must.NoError(t, err)
	must.Nil(t, getResp.Job, must.Sprintf("getResp: %#v", getResp))

	// Update the job
	job.TaskGroups[0].Count = 0
	req.Job = job
	err = msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp)
	must.NoError(t, err)

	getReq.Region = "east"
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	must.NoError(t, err)

	eastJob = getResp.Job
	must.NotNil(t, eastJob, must.Sprintf("getResp: %#v", getResp))
	must.Eq(t, 1, eastJob.TaskGroups[0].Count)

	getReq.Region = "west"
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	must.NoError(t, err)
	westJob = getResp.Job
	must.Eq(t, 2, westJob.TaskGroups[0].Count)

	must.Greater(t, oldVersion, eastJob.Version)
	must.Eq(t, eastJob.Version, westJob.Version)
	oldVersion = eastJob.Version

	// Update the job again, dropping one region
	job.Multiregion.Regions = []*structs.MultiregionRegion{
		{
			Name:        "west",
			Count:       2,
			Datacenters: []string{"west-1", "west-2"},
			Meta:        map[string]string{"region_code": "W"},
		},
	}
	req.Job = job
	req.WriteRequest.Region = "west"

	err = msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp)
	must.NoError(t, err)

	getReq.Region = "east"
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	must.NoError(t, err)
	eastJob = getResp.Job
	must.True(t, eastJob.Stopped(), must.Sprint("expected job to be stopped"))

	getReq.Region = "west"
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	must.NoError(t, err)
	westJob = getResp.Job
	must.Greater(t, oldVersion, westJob.Version)
	must.False(t, westJob.Stopped(), must.Sprint("expected job to be running"))
}

func TestJobEndpoint_Register_Multiregion_NodePool(t *testing.T) {
	ci.Parallel(t)

	// Helper function to setup client heartbeat.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	heartbeat := func(ctx context.Context, codec rpc.ClientCodec, req *structs.NodeUpdateStatusRequest) {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			default:
			}

			var resp structs.NodeUpdateResponse
			msgpackrpc.CallWithCodec(codec, "Node.UpdateStatus", req, &resp)
		}
	}

	// Create server in the authoritative region west.
	west, root, cleanupWest := TestACLServer(t, func(c *Config) {
		c.Region = "west"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.NumSchedulers = 1
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupWest()
	westCodec := rpcClient(t, west)

	// Create server in the non-authoritative region east.
	east, _, cleanupEast := TestACLServer(t, func(c *Config) {
		c.Region = "east"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.ReplicationBackoff = 20 * time.Millisecond
		c.ReplicationToken = root.SecretID
		c.NumSchedulers = 1
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupEast()
	eastCodec := rpcClient(t, east)

	// Federate regions.
	TestJoin(t, west, east)
	testutil.WaitForLeader(t, west.RPC)
	testutil.WaitForLeader(t, east.RPC)

	// Can't use Server.numPeers here b/c these are different regions.
	must.Wait(t, wait.InitialSuccess(
		wait.ErrorFunc(func() error {
			if west.serf.NumNodes() != 2 {
				return fmt.Errorf("should have 2 peers")
			}
			return nil
		}),
		wait.Timeout(3*time.Second),
		wait.Gap(100*time.Millisecond),
	))

	// Create test node pools.
	eastPool := &structs.NodePool{Name: "east-np"}
	westPool := &structs.NodePool{Name: "west-np"}

	nodePoolsReq := &structs.NodePoolUpsertRequest{
		NodePools: []*structs.NodePool{eastPool, westPool},
		WriteRequest: structs.WriteRequest{
			Region:    "west",
			AuthToken: root.SecretID,
		},
	}
	var nodePoolsResp structs.GenericResponse
	err := msgpackrpc.CallWithCodec(westCodec, "NodePool.UpsertNodePools", nodePoolsReq, &nodePoolsResp)
	must.NoError(t, err)

	// Create test nodes in each region. Each region has one node in the
	// 'default' node pool and one node in a region-specific node pool.
	nodeEast1 := mock.Node()
	nodeEast2 := mock.Node()
	nodeEast2.NodePool = eastPool.Name

	nodeWest1 := mock.Node()
	nodeWest2 := mock.Node()
	nodeWest2.NodePool = westPool.Name

	nodesPerRegion := map[string][]*structs.Node{
		"east": {nodeEast1, nodeEast2},
		"west": {nodeWest1, nodeWest2},
	}
	for region, nodes := range nodesPerRegion {
		for _, node := range nodes {
			node.Status = ""

			var codec rpc.ClientCodec
			var server *Server
			switch region {
			case "east":
				codec = eastCodec
				server = east
			case "west":
				codec = westCodec
				server = west
			}

			// Register node and setup a heartbeat goroutine.
			req := &structs.NodeRegisterRequest{
				Node:         node,
				WriteRequest: structs.WriteRequest{Region: region},
			}
			var resp structs.GenericResponse
			err := msgpackrpc.CallWithCodec(codec, "Node.Register", req, &resp)
			must.NoError(t, err)

			go heartbeat(ctx, rpcClient(t, server), &structs.NodeUpdateStatusRequest{
				NodeID:       node.ID,
				Status:       structs.NodeStatusReady,
				WriteRequest: structs.WriteRequest{Region: region},
			})
		}
	}

	// Create multiregion job in the 'default' node pool.
	job := mock.MultiregionMinJob()
	job.NodePool = structs.NodePoolDefault
	job.Update.AutoPromote = true

	req := &structs.JobRegisterRequest{
		Job: job,
		WriteRequest: structs.WriteRequest{
			Region:    "west",
			AuthToken: root.SecretID,
		},
	}
	var resp structs.JobRegisterResponse
	err = msgpackrpc.CallWithCodec(westCodec, "Job.Register", req, &resp)
	must.NoError(t, err)

	// verify is a helper function that tests if each region has one allocation
	// in the expected node pool.
	verify := func(job *structs.Job, expected map[string]string) {
		must.Wait(t, wait.InitialSuccess(
			wait.ErrorFunc(func() error {
				for region, expectedPool := range expected {
					var s *state.StateStore
					switch region {
					case "east":
						s = east.fsm.State()
					case "west":
						s = west.fsm.State()
					}

					allocs, err := s.AllocsByJob(nil, job.Namespace, job.ID, false)
					if err != nil {
						return fmt.Errorf(
							"failed to get allocs in region '%s': %v",
							region, err,
						)
					}
					if len(allocs) != 1 {
						return fmt.Errorf(
							"expected 1 allocation in region '%s', got %d",
							region, len(allocs),
						)
					}

					node, err := s.NodeByID(nil, allocs[0].NodeID)
					if node.NodePool != expectedPool {
						return fmt.Errorf(
							"expected alloc in region '%s' to be in node pool '%s', got '%s'",
							region, expectedPool, node.NodePool,
						)
					}
				}
				return nil
			}),
			wait.Timeout(10*time.Second),
			wait.Gap(1*time.Second),
		))
	}

	// Verify each region receives one allocation in the 'default' node pool.
	verify(job, map[string]string{
		"east": structs.NodePoolDefault,
		"west": structs.NodePoolDefault,
	})

	// Create multiregion job with each region in its own node pool.
	job = mock.MultiregionMinJob()
	job.Update.AutoPromote = true
	job.Multiregion.Regions[0].NodePool = westPool.Name
	job.Multiregion.Regions[1].NodePool = eastPool.Name

	req = &structs.JobRegisterRequest{
		Job: job,
		WriteRequest: structs.WriteRequest{
			Region:    "west",
			AuthToken: root.SecretID,
		},
	}
	err = msgpackrpc.CallWithCodec(westCodec, "Job.Register", req, &resp)
	must.NoError(t, err)

	// Verify each region receives one allocation in their own node pool.
	verify(job, map[string]string{
		"east": eastPool.Name,
		"west": westPool.Name,
	})

	// Create a multiregion job with only region defining a node pool.
	job = mock.MultiregionMinJob()
	job.Update.AutoPromote = true
	job.Multiregion.Regions[1].NodePool = eastPool.Name

	req = &structs.JobRegisterRequest{
		Job: job,
		WriteRequest: structs.WriteRequest{
			Region:    "west",
			AuthToken: root.SecretID,
		},
	}
	err = msgpackrpc.CallWithCodec(westCodec, "Job.Register", req, &resp)
	must.NoError(t, err)

	// Verify east region has an allocation in its node pool and the west
	// region has an allocation in the 'default' node pool.
	verify(job, map[string]string{
		"east": eastPool.Name,
		"west": structs.NodePoolDefault,
	})
}

func TestJobEndpoint_Register_Multiregion_MaxVersion(t *testing.T) {
	ci.Parallel(t)

	west, root, cleanupWest := TestACLServer(t, func(c *Config) {
		c.Region = "west"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.NumSchedulers = 1
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupWest()

	east, _, cleanupEast := TestACLServer(t, func(c *Config) {
		c.Region = "east"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.ReplicationBackoff = 20 * time.Millisecond
		c.ReplicationToken = root.SecretID
		c.NumSchedulers = 1
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupEast()

	TestJoin(t, west, east)
	testutil.WaitForLeader(t, west.RPC)
	testutil.WaitForLeader(t, east.RPC)

	codecEast := rpcClient(t, east)

	// can't use Server.numPeers here b/c these are different regions
	testutil.WaitForResult(func() (bool, error) {
		return west.serf.NumNodes() == 2, nil
	}, func(err error) {
		t.Fatalf("error waiting peering, have %v peers: %s", west.serf.NumNodes(), err.Error())
	})

	job := mock.MultiregionJob()

	// register into east, update until version 2
	initJob := job.Copy()
	initJob.Multiregion = nil
	eastRegReq := &structs.JobRegisterRequest{
		Job: initJob,
		WriteRequest: structs.WriteRequest{
			Region:    "east",
			AuthToken: root.SecretID,
		},
	}
	must.NoError(t, msgpackrpc.CallWithCodec(codecEast, "Job.Register", eastRegReq, &structs.JobRegisterResponse{}))
	initJob.Meta["take"] = "two"
	must.NoError(t, msgpackrpc.CallWithCodec(codecEast, "Job.Register", eastRegReq, &structs.JobRegisterResponse{}))
	initJob.Meta["take"] = "three"
	must.NoError(t, msgpackrpc.CallWithCodec(codecEast, "Job.Register", eastRegReq, &structs.JobRegisterResponse{}))
	eastJob, err := east.State().JobByID(nil, job.Namespace, job.ID)
	must.NoError(t, err)
	must.NotNil(t, eastJob)
	must.Eq(t, 2, eastJob.Version)
	eastJobModifyIndex := eastJob.JobModifyIndex

	// register into west with version 0
	initJob = job.Copy()
	initJob.Multiregion = nil
	westRegReq := &structs.JobRegisterRequest{
		Job: initJob,
		WriteRequest: structs.WriteRequest{
			Region:    "west",
			AuthToken: root.SecretID,
		},
	}
	must.NoError(t, msgpackrpc.CallWithCodec(codecEast, "Job.Register", westRegReq, &structs.JobRegisterResponse{}))
	westJob, err := west.State().JobByID(nil, job.Namespace, job.ID)
	must.NoError(t, err)
	must.NotNil(t, westJob)
	must.Eq(t, 0, westJob.Version)
	westJobModifyIndex := westJob.JobModifyIndex

	// Register the multiregion job; this should result in a job with synchronized versions
	multiRegReq := &structs.JobRegisterRequest{
		Job: job,
		WriteRequest: structs.WriteRequest{
			Region:    "east",
			AuthToken: root.SecretID,
		},
	}
	err = msgpackrpc.CallWithCodec(codecEast, "Job.Register", multiRegReq, &api.JobRegisterResponse{})
	must.NoError(t, err)

	// check that job versions are synchronized at 3
	eastJob, err = east.State().JobByID(nil, job.Namespace, job.ID)
	must.NoError(t, err)
	must.NotNil(t, eastJob)
	must.Eq(t, 3, eastJob.Version)
	must.Greater(t, eastJobModifyIndex, eastJob.JobModifyIndex)

	westJob, err = west.State().JobByID(nil, job.Namespace, job.ID)
	must.NoError(t, err)
	must.NotNil(t, westJob)
	must.Eq(t, 3, westJob.Version)
	must.Greater(t, westJobModifyIndex, westJob.JobModifyIndex)
}

func TestJobEndpoint_MultiregionStarter(t *testing.T) {
	ci.Parallel(t)

	j := &structs.Job{}
	j.Type = "service"
	must.True(t, jobIsMultiregionStarter(j, "north"))

	tc := &structs.Multiregion{
		Strategy: &structs.MultiregionStrategy{},
		Regions: []*structs.MultiregionRegion{
			{Name: "north"},
			{Name: "south"},
			{Name: "east"},
			{Name: "west"},
		},
	}

	b := &structs.Job{}
	b.Type = "batch"
	b.Multiregion = tc
	must.True(t, jobIsMultiregionStarter(b, "west"))

	j.Multiregion = tc
	must.True(t, jobIsMultiregionStarter(j, "north"))
	must.True(t, jobIsMultiregionStarter(j, "south"))
	must.True(t, jobIsMultiregionStarter(j, "east"))
	must.True(t, jobIsMultiregionStarter(j, "west"))

	tc.Strategy = &structs.MultiregionStrategy{MaxParallel: 1}
	j.Multiregion = tc
	must.True(t, jobIsMultiregionStarter(j, "north"))
	must.False(t, jobIsMultiregionStarter(j, "south"))
	must.False(t, jobIsMultiregionStarter(j, "east"))
	must.False(t, jobIsMultiregionStarter(j, "west"))

	tc.Strategy = &structs.MultiregionStrategy{MaxParallel: 2}
	j.Multiregion = tc
	must.True(t, jobIsMultiregionStarter(j, "north"))
	must.True(t, jobIsMultiregionStarter(j, "south"))
	must.False(t, jobIsMultiregionStarter(j, "east"))
	must.False(t, jobIsMultiregionStarter(j, "west"))
}

func TestJobEndpoint_Deregister_Multiregion(t *testing.T) {
	ci.Parallel(t)

	west, root, cleanupWest := TestACLServer(t, func(c *Config) {
		c.Region = "west"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.NumSchedulers = 1
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupWest()

	east, _, cleanupEast := TestACLServer(t, func(c *Config) {
		c.Region = "east"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.ReplicationBackoff = 20 * time.Millisecond
		c.ReplicationToken = root.SecretID
		c.NumSchedulers = 1
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupEast()

	north, _, cleanupNorth := TestACLServer(t, func(c *Config) {
		c.Region = "north"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.ReplicationBackoff = 20 * time.Millisecond
		c.ReplicationToken = root.SecretID
		c.NumSchedulers = 1
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupNorth()

	TestJoin(t, west, east, north)
	testutil.WaitForLeader(t, west.RPC)
	testutil.WaitForLeader(t, east.RPC)
	testutil.WaitForLeader(t, north.RPC)

	codec := rpcClient(t, north)

	// can't use Server.numPeers here b/c these are different regions
	testutil.WaitForResult(func() (bool, error) {
		return west.serf.NumNodes() == 3, nil
	}, func(err error) {
		t.Fatalf("should have 3 peers")
	})

	job := mock.MultiregionJob()
	job.Multiregion.Regions = append(
		job.Multiregion.Regions,
		&structs.MultiregionRegion{
			Name:        "north",
			Count:       1,
			Datacenters: []string{"north-1"},
		},
	)

	req := &structs.JobRegisterRequest{
		Job: job,
		WriteRequest: structs.WriteRequest{
			Region:    "east",
			AuthToken: root.SecretID,
		},
	}

	err := msgpackrpc.CallWithCodec(codec, "Job.Register", req,
		&structs.JobRegisterResponse{})
	must.NoError(t, err)

	assertStatus := func(region string, isRunning bool) {
		getReq := &structs.JobSpecificRequest{
			JobID: job.ID,
			QueryOptions: structs.QueryOptions{
				Region:    region,
				AuthToken: root.SecretID,
			},
		}
		var getResp structs.SingleJobResponse
		err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
		must.NoError(t, err)
		must.Eq(t, !isRunning, getResp.Job.Stopped(),
			must.Sprintf("expected %q region to be running=%v", region, isRunning))
	}

	assertStatus("east", true)
	assertStatus("west", true)
	assertStatus("north", true)

	// deregister a single region
	deReq := &structs.JobDeregisterRequest{
		JobID:  job.ID,
		Global: false,
		WriteRequest: structs.WriteRequest{
			Region:    "east",
			Namespace: job.Namespace,
			AuthToken: root.SecretID,
		},
	}
	err = msgpackrpc.CallWithCodec(codec, "Job.Deregister", deReq,
		&structs.JobDeregisterResponse{})

	assertStatus("east", false)
	assertStatus("west", true)
	assertStatus("north", true)

	deReq.Global = true
	err = msgpackrpc.CallWithCodec(codec, "Job.Deregister", deReq,
		&structs.JobDeregisterResponse{})

	assertStatus("east", false)
	assertStatus("west", false)
	assertStatus("north", false)
}

func TestJobEndpoint_Deregister_Reregister_Multiregion(t *testing.T) {
	ci.Parallel(t)

	// Asserts job stop state and version.
	assertStatusAndVersion := func(jobID, region, secretID string, codec rpc.ClientCodec, isStopped bool, version uint64) {
		getReq := &structs.JobSpecificRequest{
			JobID: jobID,
			QueryOptions: structs.QueryOptions{
				Region:    region,
				AuthToken: secretID,
			},
		}

		var getResp *structs.SingleJobResponse

		testutil.WaitForResult(func() (bool, error) {
			err := msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
			if err != nil {
				return false, err
			}

			if getResp == nil || getResp.Job == nil {
				return false, nil
			}

			return isStopped == getResp.Job.Stopped() &&
				getResp.Job.Version == version, nil

		}, func(err error) {
			must.NoError(t, err)
		})
	}

	type testCase struct {
		name          string
		stopInRegions []string
		purge         bool
	}

	testCases := []testCase{
		{
			name:          "stop and purge in non-authoritative region",
			stopInRegions: []string{"east"},
			purge:         true,
		},
		{
			name:          "stop and purge in authoritative region",
			stopInRegions: []string{"west"},
			purge:         true,
		},
		{
			name:          "stop without purge in non-authoritative region",
			stopInRegions: []string{"east"},
			purge:         false,
		},
		{
			name:          "stop without purge in authoritative region",
			stopInRegions: []string{"west"},
			purge:         false,
		},
		{
			name:          "stop and purge in multiple regions",
			stopInRegions: []string{"west", "north"},
			purge:         true,
		},
		{
			name:          "stop without purge in multiple regions",
			stopInRegions: []string{"east", "west"},
			purge:         false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			root, cleanupWest, cleanupEast, cleanupNorth, codec := buildMultiregionCluster(t)
			defer cleanupWest()
			defer cleanupEast()
			defer cleanupNorth()

			job := mock.MultiregionJob()
			job.Multiregion.Regions = append(
				job.Multiregion.Regions,
				&structs.MultiregionRegion{
					Name:        "north",
					Count:       1,
					Datacenters: []string{"north-1"},
				},
			)

			req := &structs.JobRegisterRequest{
				Job: job,
				WriteRequest: structs.WriteRequest{
					Region:    "west",
					AuthToken: root.SecretID,
				},
			}

			err := msgpackrpc.CallWithCodec(codec, "Job.Register", req,
				&structs.JobRegisterResponse{})
			must.NoError(t, err)

			assertStatusAndVersion(job.ID, "east", root.SecretID, codec, false, 0)
			assertStatusAndVersion(job.ID, "west", root.SecretID, codec, false, 0)
			assertStatusAndVersion(job.ID, "north", root.SecretID, codec, false, 0)

			// deregister target regions
			for _, stopInRegion := range tc.stopInRegions {
				deReq := &structs.JobDeregisterRequest{
					JobID:  job.ID,
					Global: false,
					Purge:  tc.purge,
					WriteRequest: structs.WriteRequest{
						Region:    stopInRegion,
						Namespace: job.Namespace,
						AuthToken: root.SecretID,
					},
				}

				deResp := &structs.JobDeregisterResponse{}
				err = msgpackrpc.CallWithCodec(codec, "Job.Deregister", deReq, deResp)
				must.NoError(t, err)
				assertStatusAndVersion(job.ID, stopInRegion, root.SecretID, codec, true, 0)
			}

			// Now re-register the job after de-registering the target regions
			err = msgpackrpc.CallWithCodec(codec, "Job.Register", req,
				&structs.JobRegisterResponse{})
			must.NoError(t, err)

			assertStatusAndVersion(job.ID, "east", root.SecretID, codec, false, 1)
			assertStatusAndVersion(job.ID, "west", root.SecretID, codec, false, 1)
			assertStatusAndVersion(job.ID, "north", root.SecretID, codec, false, 1)
		})
	}
}

func buildMultiregionCluster(t *testing.T) (*structs.ACLToken, func(), func(), func(), rpc.ClientCodec) {
	west, root, cleanupWest := TestACLServer(t, func(c *Config) {
		c.Region = "west"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.NumSchedulers = 1
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})

	east, _, cleanupEast := TestACLServer(t, func(c *Config) {
		c.Region = "east"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.ReplicationBackoff = 20 * time.Millisecond
		c.ReplicationToken = root.SecretID
		c.NumSchedulers = 1
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})

	north, _, cleanupNorth := TestACLServer(t, func(c *Config) {
		c.Region = "north"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.ReplicationBackoff = 20 * time.Millisecond
		c.ReplicationToken = root.SecretID
		c.NumSchedulers = 1
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})

	TestJoin(t, west, east, north)
	testutil.WaitForLeader(t, west.RPC)
	testutil.WaitForLeader(t, east.RPC)
	testutil.WaitForLeader(t, north.RPC)

	codec := rpcClient(t, north)

	// can't use Server.numPeers here b/c these are different regions
	testutil.WaitForResult(func() (bool, error) {
		return west.serf.NumNodes() == 3, nil
	}, func(err error) {
		must.NoError(t, err, must.Sprint("should have 3 peers"))
	})
	return root, cleanupWest, cleanupEast, cleanupNorth, codec
}

// TestJobEndpoint_Register_Connect_AllowUnauthenticatedFalse asserts that a job
// submission fails allow_unauthenticated is false, and either an invalid or no
// operator Consul token is provided.
func TestJobEndpoint_Register_Connect_AllowUnauthenticatedFalse_ent(t *testing.T) {
	ci.Parallel(t)

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.NumSchedulers = 0 // Prevent automatic dequeue
		c.ConsulConfig.AllowUnauthenticated = pointer.Of(false)
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	newJob := func(namespace string) *structs.Job {
		// Create the register request
		job := mock.Job()
		job.TaskGroups[0].Networks[0].Mode = "bridge"
		job.TaskGroups[0].Services = []*structs.Service{
			{
				Name:      "service1", // matches consul.ExamplePolicyID1
				PortLabel: "8080",
				Connect: &structs.ConsulConnect{
					SidecarService: &structs.ConsulSidecarService{},
				},
			},
		}
		// For this test we only care about authorizing the connect service
		job.TaskGroups[0].Tasks[0].Services = nil

		// If testing with a Consul namespace, set it on the group
		if namespace != "" {
			job.TaskGroups[0].Consul = &structs.Consul{
				Namespace: namespace,
			}
		}
		return job
	}

	newRequest := func(job *structs.Job) *structs.JobRegisterRequest {
		return &structs.JobRegisterRequest{
			Job: job,
			WriteRequest: structs.WriteRequest{
				Region:    "global",
				Namespace: job.Namespace,
			},
		}
	}

	noTokenOnJob := func(t *testing.T, job *structs.Job) {
		fsmState := s1.State()
		ws := memdb.NewWatchSet()
		storedJob, err := fsmState.JobByID(ws, job.Namespace, job.ID)
		must.NoError(t, err)
		must.NotNil(t, storedJob)
		must.Eq(t, storedJob.ConsulToken, "")
	}

	// Non-sense Consul ACL tokens that should be rejected
	missingToken := ""
	fakeToken := uuid.Generate()

	// Consul ACL tokens in no Consul namespace
	ossTokenNoPolicyNoNS := consul.ExampleOperatorTokenID3
	ossTokenNoNS := consul.ExampleOperatorTokenID1

	// Consul ACL tokens in "default" Consul namespace
	entTokenNoPolicyDefaultNS := consul.ExampleOperatorTokenID20
	entTokenDefaultNS := consul.ExampleOperatorTokenID21

	// Consul ACL tokens in "banana" Consul namespace
	entTokenNoPolicyBananaNS := consul.ExampleOperatorTokenID10
	entTokenBananaNS := consul.ExampleOperatorTokenID11

	t.Run("group consul namespace unset", func(t *testing.T) {
		// When the group namespace is unset, Nomad pretends like it belongs to
		// the "default" namespace. Consul tokens must be in the "default" namespace.
		namespace := ""

		t.Run("no token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = missingToken
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, "job-submitter consul token denied: missing consul token")
		})

		t.Run("unknown token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = fakeToken
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, "job-submitter consul token denied: unable to read consul token: no such token")
		})

		t.Run("unauthorized oss token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = ossTokenNoPolicyNoNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: insufficient Consul ACL permissions to write service "service1"`)
		})

		t.Run("authorized oss token provided", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = ossTokenNoNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.NoError(t, err)
			noTokenOnJob(t, job)
		})

		t.Run("unauthorized token in default namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenNoPolicyDefaultNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: insufficient Consul ACL permissions to write service "service1"`)
		})

		t.Run("authorized token in default namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenDefaultNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.NoError(t, err)
			noTokenOnJob(t, job)
		})

		t.Run("unauthorized token in banana namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenNoPolicyBananaNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: consul ACL token requires using namespace "banana"`)
		})

		t.Run("authorized token in banana namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenBananaNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: consul ACL token requires using namespace "banana"`)
		})
	})

	t.Run("group consul namespace banana", func(t *testing.T) {

		// Nomad ENT will respect the group.consul.namespace, and as such the
		// Consul ACL token must belong to that namespace OR be blessed with
		// a namespace or namespace_prefix block with otherwise sufficient
		// service/service_prefix/kv_prefix write policies.

		namespace := "banana"

		t.Run("no token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = missingToken
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, "job-submitter consul token denied: missing consul token")
		})

		t.Run("unknown token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = fakeToken
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, "job-submitter consul token denied: unable to read consul token: no such token")
		})

		t.Run("unauthorized oss token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = ossTokenNoPolicyNoNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: consul ACL token cannot use namespace "banana"`)
		})

		t.Run("authorized oss token provided", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = ossTokenNoNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: consul ACL token cannot use namespace "banana"`)
		})

		t.Run("unauthorized token in default namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenNoPolicyDefaultNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: insufficient Consul ACL permissions to write service "service1"`)
		})

		t.Run("authorized token in default namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenDefaultNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: insufficient Consul ACL permissions to write service "service1"`)
		})

		t.Run("unauthorized token in banana namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenNoPolicyBananaNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: insufficient Consul ACL permissions to write service "service1"`)
		})

		t.Run("authorized token in banana namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenBananaNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.NoError(t, err)
			noTokenOnJob(t, job)
		})
	})

	t.Run("group consul namespace default", func(t *testing.T) {

		// Nomad ENT will respect the namespace, and will not accept Consul OSS
		// ACL tokens for the default namespace.
		namespace := "default"

		t.Run("no token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = missingToken
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, "job-submitter consul token denied: missing consul token")
		})

		t.Run("unknown token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = fakeToken
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, "job-submitter consul token denied: unable to read consul token: no such token")
		})

		t.Run("unauthorized oss token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = ossTokenNoPolicyNoNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: consul ACL token cannot use namespace "default"`)
		})

		t.Run("authorized oss token provided", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = ossTokenNoNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: consul ACL token cannot use namespace "default"`)
		})

		t.Run("unauthorized token in default namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenNoPolicyDefaultNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: insufficient Consul ACL permissions to write service "service1"`)
		})

		t.Run("authorized token in default namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenDefaultNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.NoError(t, err)
			noTokenOnJob(t, job)
		})

		t.Run("unauthorized token in banana namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenNoPolicyBananaNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: consul ACL token cannot use namespace "default"`)
		})

		t.Run("authorized token in banana namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenBananaNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			must.EqError(t, err, `job-submitter consul token denied: consul ACL token cannot use namespace "default"`)
		})
	})
}
