//go:build ent
// +build ent

package nomad

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-memdb"
	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/command/agent/consul"
	"github.com/hashicorp/nomad/helper"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
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

func TestJobEndpoint_Register_Multiregion(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	west, root, cleanupWest := TestACLServer(t, func(c *Config) {
		c.Region = "west"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.NumSchedulers = 1
		c.LicenseEnv = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupWest()

	east, _, cleanupEast := TestACLServer(t, func(c *Config) {
		c.Region = "east"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.ReplicationBackoff = 20 * time.Millisecond
		c.ReplicationToken = root.SecretID
		c.NumSchedulers = 1
		c.LicenseEnv = licenseForMulticlusterEfficiency().Signed
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
		c.LicenseEnv = licenseForMulticlusterEfficiency().Signed
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
	require.EqualValues(0, eastJob.Version)

	getReq.Region = "west"
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	require.NoError(err)
	westJob := getResp.Job

	require.NotNil(westJob, fmt.Sprintf("getResp: %#v", getResp))
	require.Equal("west", westJob.Region)
	require.Equal([]string{"west-1", "west-2"}, westJob.Datacenters)
	require.Equal("W", westJob.Meta["region_code"])
	require.Equal(10, westJob.TaskGroups[0].Count)
	require.EqualValues(westJob.Version, eastJob.Version)
	oldVersion := westJob.Version

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

	require.Greater(eastJob.Version, oldVersion)
	require.EqualValues(eastJob.Version, westJob.Version)
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
	require.NoError(err)

	getReq.Region = "east"
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	require.NoError(err)
	eastJob = getResp.Job
	require.True(eastJob.Stopped(), "expected job to be stopped")

	getReq.Region = "west"
	err = msgpackrpc.CallWithCodec(codec, "Job.GetJob", getReq, &getResp)
	require.NoError(err)
	westJob = getResp.Job
	require.Greater(westJob.Version, oldVersion)
	require.False(westJob.Stopped(), "expected job to be running")
}

func TestJobEndpoint_Register_Multiregion_MaxVersion(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	west, root, cleanupWest := TestACLServer(t, func(c *Config) {
		c.Region = "west"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.NumSchedulers = 1
		c.LicenseEnv = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupWest()

	east, _, cleanupEast := TestACLServer(t, func(c *Config) {
		c.Region = "east"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.ReplicationBackoff = 20 * time.Millisecond
		c.ReplicationToken = root.SecretID
		c.NumSchedulers = 1
		c.LicenseEnv = licenseForMulticlusterEfficiency().Signed
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
	require.NoError(msgpackrpc.CallWithCodec(codecEast, "Job.Register", eastRegReq, &structs.JobRegisterResponse{}))
	initJob.Meta["take"] = "two"
	require.NoError(msgpackrpc.CallWithCodec(codecEast, "Job.Register", eastRegReq, &structs.JobRegisterResponse{}))
	initJob.Meta["take"] = "three"
	require.NoError(msgpackrpc.CallWithCodec(codecEast, "Job.Register", eastRegReq, &structs.JobRegisterResponse{}))
	eastJob, err := east.State().JobByID(nil, job.Namespace, job.ID)
	require.NoError(err)
	require.NotNil(eastJob)
	require.EqualValues(2, eastJob.Version)
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
	require.NoError(msgpackrpc.CallWithCodec(codecEast, "Job.Register", westRegReq, &structs.JobRegisterResponse{}))
	westJob, err := west.State().JobByID(nil, job.Namespace, job.ID)
	require.NoError(err)
	require.NotNil(westJob)
	require.EqualValues(0, westJob.Version)
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
	require.NoError(err)

	// check that job versions are synchronized at 3
	eastJob, err = east.State().JobByID(nil, job.Namespace, job.ID)
	require.NoError(err)
	require.NotNil(eastJob)
	require.EqualValues(3, eastJob.Version)
	require.Greater(eastJob.JobModifyIndex, eastJobModifyIndex)

	westJob, err = west.State().JobByID(nil, job.Namespace, job.ID)
	require.NoError(err)
	require.NotNil(westJob)
	require.EqualValues(3, westJob.Version)
	require.Greater(westJob.JobModifyIndex, westJobModifyIndex)
}

func TestJobEndpoint_MultiregionStarter(t *testing.T) {
	require := require.New(t)

	j := &structs.Job{}
	j.Type = "service"
	require.True(jobIsMultiregionStarter(j, "north"))

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
	require.True(jobIsMultiregionStarter(b, "west"))

	j.Multiregion = tc
	require.True(jobIsMultiregionStarter(j, "north"))
	require.True(jobIsMultiregionStarter(j, "south"))
	require.True(jobIsMultiregionStarter(j, "east"))
	require.True(jobIsMultiregionStarter(j, "west"))

	tc.Strategy = &structs.MultiregionStrategy{MaxParallel: 1}
	j.Multiregion = tc
	require.True(jobIsMultiregionStarter(j, "north"))
	require.False(jobIsMultiregionStarter(j, "south"))
	require.False(jobIsMultiregionStarter(j, "east"))
	require.False(jobIsMultiregionStarter(j, "west"))

	tc.Strategy = &structs.MultiregionStrategy{MaxParallel: 2}
	j.Multiregion = tc
	require.True(jobIsMultiregionStarter(j, "north"))
	require.True(jobIsMultiregionStarter(j, "south"))
	require.False(jobIsMultiregionStarter(j, "east"))
	require.False(jobIsMultiregionStarter(j, "west"))
}

func TestJobEndpoint_Deregister_Multiregion(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	west, root, cleanupWest := TestACLServer(t, func(c *Config) {
		c.Region = "west"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.NumSchedulers = 1
		c.LicenseEnv = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupWest()

	east, _, cleanupEast := TestACLServer(t, func(c *Config) {
		c.Region = "east"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.ReplicationBackoff = 20 * time.Millisecond
		c.ReplicationToken = root.SecretID
		c.NumSchedulers = 1
		c.LicenseEnv = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupEast()

	north, _, cleanupNorth := TestACLServer(t, func(c *Config) {
		c.Region = "north"
		c.AuthoritativeRegion = "west"
		c.ACLEnabled = true
		c.ReplicationBackoff = 20 * time.Millisecond
		c.ReplicationToken = root.SecretID
		c.NumSchedulers = 1
		c.LicenseEnv = licenseForMulticlusterEfficiency().Signed
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
	require.NoError(err)

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
		require.NoError(err)
		require.Equal(!isRunning, getResp.Job.Stopped(),
			"expected %q region to be running=%v", region, isRunning)
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

// TestJobEndpoint_Register_Connect_AllowUnauthenticatedFalse asserts that a job
// submission fails allow_unauthenticated is false, and either an invalid or no
// operator Consul token is provided.
func TestJobEndpoint_Register_Connect_AllowUnauthenticatedFalse_ent(t *testing.T) {
	t.Parallel()

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.NumSchedulers = 0 // Prevent automatic dequeue
		c.ConsulConfig.AllowUnauthenticated = helper.BoolToPtr(false)
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
		require.NoError(t, err)
		require.NotNil(t, storedJob)
		require.Empty(t, storedJob.ConsulToken)
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
			require.EqualError(t, err, "job-submitter consul token denied: missing consul token")
		})

		t.Run("unknown token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = fakeToken
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, "job-submitter consul token denied: unable to read consul token: no such token")
		})

		t.Run("unauthorized oss token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = ossTokenNoPolicyNoNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: insufficient Consul ACL permissions to write service "service1"`)
		})

		t.Run("authorized oss token provided", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = ossTokenNoNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.NoError(t, err)
			noTokenOnJob(t, job)
		})

		t.Run("unauthorized token in default namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenNoPolicyDefaultNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: insufficient Consul ACL permissions to write service "service1"`)
		})

		t.Run("authorized token in default namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenDefaultNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.NoError(t, err)
			noTokenOnJob(t, job)
		})

		t.Run("unauthorized token in banana namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenNoPolicyBananaNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: consul ACL token requires using namespace "banana"`)
		})

		t.Run("authorized token in banana namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenBananaNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: consul ACL token requires using namespace "banana"`)
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
			require.EqualError(t, err, "job-submitter consul token denied: missing consul token")
		})

		t.Run("unknown token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = fakeToken
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, "job-submitter consul token denied: unable to read consul token: no such token")
		})

		t.Run("unauthorized oss token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = ossTokenNoPolicyNoNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: consul ACL token cannot use namespace "banana"`)
		})

		t.Run("authorized oss token provided", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = ossTokenNoNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: consul ACL token cannot use namespace "banana"`)
		})

		t.Run("unauthorized token in default namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenNoPolicyDefaultNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: insufficient Consul ACL permissions to write service "service1"`)
		})

		t.Run("authorized token in default namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenDefaultNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: insufficient Consul ACL permissions to write service "service1"`)
		})

		t.Run("unauthorized token in banana namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenNoPolicyBananaNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: insufficient Consul ACL permissions to write service "service1"`)
		})

		t.Run("authorized token in banana namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenBananaNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.NoError(t, err)
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
			require.EqualError(t, err, "job-submitter consul token denied: missing consul token")
		})

		t.Run("unknown token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = fakeToken
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, "job-submitter consul token denied: unable to read consul token: no such token")
		})

		t.Run("unauthorized oss token provided", func(t *testing.T) {
			request := newRequest(newJob(namespace))
			request.Job.ConsulToken = ossTokenNoPolicyNoNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: consul ACL token cannot use namespace "default"`)
		})

		t.Run("authorized oss token provided", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = ossTokenNoNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: consul ACL token cannot use namespace "default"`)
		})

		t.Run("unauthorized token in default namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenNoPolicyDefaultNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: insufficient Consul ACL permissions to write service "service1"`)
		})

		t.Run("authorized token in default namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenDefaultNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.NoError(t, err)
			noTokenOnJob(t, job)
		})

		t.Run("unauthorized token in banana namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenNoPolicyBananaNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: consul ACL token cannot use namespace "default"`)
		})

		t.Run("authorized token in banana namespace", func(t *testing.T) {
			job := newJob(namespace)
			request := newRequest(job)
			request.Job.ConsulToken = entTokenBananaNS
			var response structs.JobRegisterResponse
			err := msgpackrpc.CallWithCodec(codec, "Job.Register", request, &response)
			require.EqualError(t, err, `job-submitter consul token denied: consul ACL token cannot use namespace "default"`)
		})
	})
}
