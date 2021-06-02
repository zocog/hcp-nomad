// +build ent

package nomad

import (
	"testing"

	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanApply_EvalPlanQuota_Under(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)
	state := testStateStore(t)

	// Create the quota spec
	qs := mock.QuotaSpec()
	assert.Nil(state.UpsertQuotaSpecs(100, []*structs.QuotaSpec{qs}))

	// Create the namespace
	ns := mock.Namespace()
	ns.Quota = qs.Name
	assert.Nil(state.UpsertNamespaces(200, []*structs.Namespace{ns}))

	// Create the job
	job := mock.Job()
	job.Namespace = ns.Name

	// Create the node
	node := mock.Node()
	state.UpsertNode(structs.MsgTypeTestSetup, 300, node)

	alloc := mock.Alloc()
	alloc.Namespace = ns.Name
	plan := &structs.Plan{
		EvalID: uuid.Generate(),
		Job:    job,
		NodeAllocation: map[string][]*structs.Allocation{
			node.ID: {alloc},
		},
	}

	snap, _ := state.Snapshot()
	over, err := evaluatePlanQuota(snap, plan)
	assert.Nil(err)
	assert.False(over)
}

func TestPlanApply_EvalPlanQuota_Above(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)
	state := testStateStore(t)

	// Create the quota spec
	qs := mock.QuotaSpec()
	assert.Nil(state.UpsertQuotaSpecs(100, []*structs.QuotaSpec{qs}))

	// Create the namespace
	ns := mock.Namespace()
	ns.Quota = qs.Name
	assert.Nil(state.UpsertNamespaces(200, []*structs.Namespace{ns}))

	// Create the job
	job := mock.Job()
	job.Namespace = ns.Name

	// Create the node
	node := mock.Node()
	state.UpsertNode(structs.MsgTypeTestSetup, 300, node)

	// Create an alloc that exceeds quota
	alloc := mock.Alloc()
	alloc.Namespace = ns.Name
	alloc.TaskResources["web"].CPU = 3000
	plan := &structs.Plan{
		EvalID: uuid.Generate(),
		Job:    job,
		NodeAllocation: map[string][]*structs.Allocation{
			node.ID: {alloc},
		},
	}

	snap, _ := state.Snapshot()
	over, err := evaluatePlanQuota(snap, plan)
	assert.Nil(err)
	assert.True(over)
}

func TestPlanApply_EvalPlan_AboveQuota(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)
	state := testStateStore(t)

	// Create the quota spec
	qs := mock.QuotaSpec()
	assert.Nil(state.UpsertQuotaSpecs(100, []*structs.QuotaSpec{qs}))

	// Create the namespace
	ns := mock.Namespace()
	ns.Quota = qs.Name
	assert.Nil(state.UpsertNamespaces(200, []*structs.Namespace{ns}))

	// Create the job
	job := mock.Job()
	job.Namespace = ns.Name

	// Create the node
	node := mock.Node()
	state.UpsertNode(structs.MsgTypeTestSetup, 1000, node)
	snap, _ := state.Snapshot()

	// Create an alloc that exceeds quota
	alloc := mock.Alloc()
	alloc.Namespace = ns.Name
	alloc.TaskResources["web"].CPU = 3000

	plan := &structs.Plan{
		EvalID: uuid.Generate(),
		Job:    job,
		NodeAllocation: map[string][]*structs.Allocation{
			node.ID: {alloc},
		},
		Deployment: mock.Deployment(),
		DeploymentUpdates: []*structs.DeploymentStatusUpdate{
			{
				DeploymentID:      uuid.Generate(),
				Status:            "foo",
				StatusDescription: "bar",
			},
		},
	}

	pool := NewEvaluatePool(workerPoolSize, workerPoolBufferSize)
	defer pool.Shutdown()

	result, err := evaluatePlan(pool, snap, plan, testlog.HCLogger(t))
	assert.Nil(err)
	assert.NotNil(result)
	assert.Empty(result.NodeAllocation)
	assert.EqualValues(1000, result.RefreshIndex)
	assert.Nil(result.Deployment)
	assert.Empty(result.DeploymentUpdates)
}

func TestPlanApply_EvalPlanQuota_NilJob(t *testing.T) {
	t.Parallel()
	assert := assert.New(t)
	state := testStateStore(t)

	// Create the quota spec
	qs := mock.QuotaSpec()
	assert.Nil(state.UpsertQuotaSpecs(100, []*structs.QuotaSpec{qs}))

	// Create the namespace
	ns := mock.Namespace()
	ns.Quota = qs.Name
	assert.Nil(state.UpsertNamespaces(200, []*structs.Namespace{ns}))

	// Create the node
	node := mock.Node()
	state.UpsertNode(structs.MsgTypeTestSetup, 300, node)

	alloc := mock.Alloc()
	alloc.Namespace = ns.Name
	plan := &structs.Plan{
		EvalID: uuid.Generate(),
		NodeAllocation: map[string][]*structs.Allocation{
			node.ID: {alloc},
		},
	}

	snap, _ := state.Snapshot()
	over, err := evaluatePlanQuota(snap, plan)
	assert.Nil(err)
	assert.False(over)
}

// TestPlanApply_EvalPlan_PriorFailedDeploy verifies that when a previous
// deployment fails we don't double-count credits for failed-but-not-removed
// allocations
func TestPlanApply_EvalPlan_PriorFailedDeploy(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	state := testStateStore(t)

	// Create the quota spec
	qs := mock.QuotaSpec()
	require.Nil(state.UpsertQuotaSpecs(100, []*structs.QuotaSpec{qs}))

	// Create the namespace
	ns := mock.Namespace()
	ns.Quota = qs.Name
	require.Nil(state.UpsertNamespaces(200, []*structs.Namespace{ns}))

	// Create the node
	node := mock.Node()
	state.UpsertNode(structs.MsgTypeTestSetup, 1000, node)

	// Create the job
	job := mock.Job()
	job.Namespace = ns.Name
	job.TaskGroups[0].Count = 4
	job.TaskGroups[0].Tasks[0].Resources.CPU = 1200

	allocs := []*structs.Allocation{}
	for i := 0; i < 5; i++ {
		alloc := mock.Alloc()
		alloc.Job = job
		alloc.Namespace = ns.Name
		alloc.NodeID = node.ID

		// assign resources; make sure legacy matches new and that we don't
		// have port collisions
		alloc.TaskResources["web"].CPU = 1200
		alloc.Resources.CPU = 1200
		alloc.TaskResources["web"].Networks = []*structs.NetworkResource{}
		alloc.AllocatedResources.Tasks["web"].Networks = []*structs.NetworkResource{}
		alloc.Resources.Networks = []*structs.NetworkResource{}

		allocs = append(allocs, alloc)
	}

	// Failed alloc but the client status hasn't been updated; should be subtracted
	allocs[0].ClientStatus = structs.AllocClientStatusRunning
	allocs[0].DesiredStatus = structs.AllocDesiredStatusStop
	allocs[0].NodeID = ""

	// Failed alloc but the client has been updated; should not be subtracted
	allocs[1].ClientStatus = structs.AllocClientStatusFailed
	allocs[1].DesiredStatus = structs.AllocDesiredStatusStop
	allocs[1].NodeID = ""

	// Happy alloc from previous run, already accounted for
	allocs[2].ClientStatus = structs.AllocClientStatusRunning
	allocs[2].DesiredStatus = structs.AllocDesiredStatusRun
	allocs[2].CreateIndex = 15

	// New allocs
	allocs[3].ClientStatus = structs.AllocClientStatusPending
	allocs[3].DesiredStatus = structs.AllocDesiredStatusRun
	allocs[4].ClientStatus = structs.AllocClientStatusPending
	allocs[4].DesiredStatus = structs.AllocDesiredStatusRun

	state.UpsertAllocs(structs.MsgTypeTestSetup, 1001, allocs[:3])
	snap, _ := state.Snapshot()

	usage, err := snap.QuotaUsageByName(nil, qs.Name)
	require.NoError(err)
	require.Equal(1200, usage.Used[string(qs.Limits[0].Hash)].RegionLimit.CPU)

	// usage is 1200:
	// +0   alloc[0] is dead
	// +0   alloc[1] is failed-but-not-updated yet
	// +1200 alloc[2] is running

	// plan + usage is still 1200 and not over quota:
	// +1200 for existing usage
	// -1200 alloc[1] is failed-but-not-updated yet
	// +0   alloc[2] is inplace
	// +1200 alloc[3] is new

	plan := &structs.Plan{
		EvalID: uuid.Generate(),
		Job:    job,
		NodeUpdate: map[string][]*structs.Allocation{
			node.ID: {allocs[0], allocs[1]},
		},
		NodeAllocation: map[string][]*structs.Allocation{
			node.ID: {allocs[2], allocs[3]},
		},
		Deployment: mock.Deployment(),
		DeploymentUpdates: []*structs.DeploymentStatusUpdate{
			{
				DeploymentID:      uuid.Generate(),
				Status:            "foo",
				StatusDescription: "bar",
			},
		},
	}

	pool := NewEvaluatePool(workerPoolSize, workerPoolBufferSize)
	defer pool.Shutdown()

	result, err := evaluatePlan(pool, snap, plan, testlog.HCLogger(t))
	require.NoError(err)
	require.NotNil(result)
	require.EqualValues(0, result.RefreshIndex,
		"fully-applied plan should not require scheduler to refresh state")
	require.Len(result.NodeAllocation[node.ID], 2)

	// Modify plan to go over quota
	plan.NodeAllocation[node.ID] = []*structs.Allocation{allocs[2], allocs[3], allocs[4]}

	// usage is still 1200:
	// +0   alloc[0] is dead
	// +0   alloc[1] is failed-but-not-updated yet
	// +1200 alloc[2] is running

	// plan + usage is now 2400 and over quota:
	// +1200 for existing usage
	// -1200 alloc[1] is failed-but-not-updated yet
	// +0   alloc[2] is inplace
	// +1200 alloc[3] is new
	// +1200 alloc[4] is new

	result, err = evaluatePlan(pool, snap, plan, testlog.HCLogger(t))
	require.NoError(err)
	require.NotNil(result)
	require.EqualValues(1001, result.RefreshIndex,
		"partially-applied plan should require scheduler to refresh state")
	require.Empty(result.NodeAllocation, "plan should not have applied fully")
}
