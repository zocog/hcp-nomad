// +build ent

package nomad

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-memdb"
	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobEndpoint_Register_Sentinel(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

func TestJobEndpoint_Plan_Sentinel(t *testing.T) {
	t.Parallel()
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

func TestJobEndpoint_Register_ACL_Namespace(t *testing.T) {
	t.Parallel()
	s1, _, cleanupS1 := TestACLServer(t, func(c *Config) {
		c.NumSchedulers = 0 // Prevent automatic dequeue
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Policy with read on default namespace and write on non default
	policy := &structs.ACLPolicy{
		Name:        fmt.Sprintf("policy-%s", uuid.Generate()),
		Description: "Super cool policy!",
		Rules: `
		namespace "default" {
			policy = "read"
		}
		namespace "test" {
			policy = "write"
		}
		node {
			policy = "read"
		}
		agent {
			policy = "read"
		}
		`,
		CreateIndex: 10,
		ModifyIndex: 20,
	}
	policy.SetHash()

	assert := assert.New(t)

	// Upsert policy and token
	token := mock.ACLToken()
	token.Policies = []string{policy.Name}
	err := s1.State().UpsertACLPolicies(100, []*structs.ACLPolicy{policy})
	assert.Nil(err)

	err = s1.State().UpsertACLTokens(110, []*structs.ACLToken{token})
	assert.Nil(err)

	// Upsert namespace
	ns := mock.Namespace()
	ns.Name = "test"
	err = s1.fsm.State().UpsertNamespaces(1000, []*structs.Namespace{ns})
	assert.Nil(err)

	// Create the register request
	job := mock.Job()
	req := &structs.JobRegisterRequest{
		Job:          job,
		WriteRequest: structs.WriteRequest{Region: "global"},
	}
	req.AuthToken = token.SecretID
	// Use token without write access to default namespace, expect failure
	var resp structs.JobRegisterResponse
	err = msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp)
	assert.NotNil(err, "expected permission denied")

	req.Namespace = "test"
	job.Namespace = "test"

	// Use token with write access to default namespace, expect success
	err = msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp)
	assert.Nil(err, "unexpected err: %v", err)
	assert.NotEqual(resp.Index, 0, "bad index: %d", resp.Index)

	// Check for the node in the FSM
	state := s1.fsm.State()
	ws := memdb.NewWatchSet()
	out, err := state.JobByID(ws, job.Namespace, job.ID)
	assert.Nil(err)
	assert.NotNil(out, "expected job")
}

func TestJobEndpoint_Register_Multiregion(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	west, root, cleanupWest := TestACLServer(t, func(c *Config) {
		c.Region = "west"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.NumSchedulers = 0 // Prevent automatic dequeue
	})
	defer cleanupWest()

	east, _, cleanupEast := TestACLServer(t, func(c *Config) {
		c.Region = "east"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.ReplicationBackoff = 20 * time.Millisecond
		c.ReplicationToken = root.SecretID
		c.NumSchedulers = 0 // Prevent automatic dequeue
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
	require.NoError(err)

	getReq := &structs.JobSpecificRequest{
		JobID: job.ID,
		QueryOptions: structs.QueryOptions{
			Region:    "east",
			AuthToken: root.SecretID,
		},
	}
	var getResp structs.SingleJobResponse
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	require.NoError(err)

	eastJob := getResp.Job
	require.NotNil(eastJob, fmt.Sprintf("getResp: %#v", getResp))
	require.Equal("east", eastJob.Region)
	require.Equal([]string{"east-1"}, eastJob.Datacenters)
	require.Equal("E", eastJob.Meta["region_code"])
	require.Equal(10, eastJob.TaskGroups[0].Count)

	getReq.Region = "west"
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	require.NoError(err)
	westJob := getResp.Job

	require.NotNil(westJob, fmt.Sprintf("getResp: %#v", getResp))
	require.Equal("west", westJob.Region)
	require.Equal([]string{"west-1", "west-2"}, westJob.Datacenters)
	require.Equal("W", westJob.Meta["region_code"])
	require.Equal(10, westJob.TaskGroups[0].Count)

	getReq.Region = "north"
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	require.NoError(err)
	require.Nil(getResp.Job, fmt.Sprintf("getResp: %#v", getResp))

	// Update the job
	job.TaskGroups[0].Count = 0
	req.Job = job
	err = msgpackrpc.CallWithCodec(codec, "Job.Register", req, &resp)
	require.NoError(err)

	getReq.Region = "east"
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	require.NoError(err)

	eastJob = getResp.Job
	require.NotNil(eastJob, fmt.Sprintf("getResp: %#v", getResp))
	require.Equal(1, eastJob.TaskGroups[0].Count)

	getReq.Region = "west"
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	require.NoError(err)
	westJob = getResp.Job
	require.Equal(2, westJob.TaskGroups[0].Count)

	require.Equal(westJob.Version, eastJob.Version)
}
