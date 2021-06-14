// +build ent

package scheduler

import (
	"fmt"
	"testing"

	memdb "github.com/hashicorp/go-memdb"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test that scaling up a job in a way that will cause the job to exceed the
// quota limit places the maximum number of allocations
func TestServiceSched_JobModify_IncrCount_QuotaLimit(t *testing.T) {
	assert := assert.New(t)
	h := NewHarness(t)

	// Create the quota spec
	qs := mock.QuotaSpec()
	assert.Nil(h.State.UpsertQuotaSpecs(h.NextIndex(), []*structs.QuotaSpec{qs}))

	// Create the namespace
	ns := mock.Namespace()
	ns.Quota = qs.Name
	assert.Nil(h.State.UpsertNamespaces(h.NextIndex(), []*structs.Namespace{ns}))

	// Create the job with two task groups with slightly different resource
	// requirements
	job := mock.Job()
	job.Namespace = ns.Name
	job.TaskGroups = append(job.TaskGroups, job.TaskGroups[0].Copy())

	job.TaskGroups[0].Count = 2
	r1 := job.TaskGroups[0].Tasks[0].Resources
	r1.CPU = 500
	r1.MemoryMB = 256
	r1.Networks = nil

	// Quota Limit: (2000 CPU, 2000 MB)
	// Total Usage at count 2 : (1000, 512)
	// Should be able to place 4 o
	// Quota would be (2000, 1024)
	assert.Nil(h.State.UpsertJob(structs.MsgTypeTestSetup, h.NextIndex(), job))

	// Create several node
	var nodes []*structs.Node
	for i := 0; i < 10; i++ {
		nodes = append(nodes, mock.Node())
		assert.Nil(h.State.UpsertNode(structs.MsgTypeTestSetup, h.NextIndex(), nodes[i]))
	}

	var allocs []*structs.Allocation
	for i := 0; i < 2; i++ {
		alloc := mock.Alloc()
		alloc.Job = job
		alloc.JobID = job.ID
		alloc.NodeID = nodes[i].ID
		alloc.Namespace = ns.Name
		alloc.TaskGroup = job.TaskGroups[0].Name
		alloc.Name = fmt.Sprintf("%s.%s[%d]", job.ID, alloc.TaskGroup, i)
		alloc.Resources = r1.Copy()
		alloc.TaskResources = map[string]*structs.Resources{
			"web": r1.Copy(),
		}
		allocs = append(allocs, alloc)
	}
	require.NoError(t, h.State.UpsertAllocs(structs.MsgTypeTestSetup, h.NextIndex(), allocs))

	// Update the task group count to 10 each
	job2 := job.Copy()
	job2.TaskGroups[0].Count = 10
	require.NoError(t, h.State.UpsertJob(structs.MsgTypeTestSetup, h.NextIndex(), job2))

	// Create a mock evaluation to deal with drain
	eval := &structs.Evaluation{
		Namespace:   ns.Name,
		ID:          uuid.Generate(),
		Priority:    50,
		TriggeredBy: structs.EvalTriggerJobRegister,
		JobID:       job.ID,
		Status:      structs.EvalStatusPending,
	}
	require.NoError(t, h.State.UpsertEvals(structs.MsgTypeTestSetup, h.NextIndex(), []*structs.Evaluation{eval}))

	// Process the evaluation
	assert.Nil(h.Process(NewServiceScheduler, eval))

	// Ensure a single plan
	assert.Len(h.Plans, 1)
	plan := h.Plans[0]

	// Ensure the plan didn't evicted the alloc
	var update []*structs.Allocation
	for _, updateList := range plan.NodeUpdate {
		update = append(update, updateList...)
	}
	assert.Empty(update)

	// Ensure the plan allocated
	var planned []*structs.Allocation
	for _, allocList := range plan.NodeAllocation {
		planned = append(planned, allocList...)
	}
	assert.Len(planned, 4)

	// Ensure the plan had a failures
	assert.Len(h.Evals, 1)

	// Ensure the eval has spawned blocked eval
	assert.Len(h.CreateEvals, 1)

	// Ensure that eval says that it was because of a quota limit
	blocked := h.CreateEvals[0]
	assert.Equal(qs.Name, blocked.QuotaLimitReached)

	// Lookup the allocations by JobID and make sure we have the right amount of
	// each type
	ws := memdb.NewWatchSet()
	out, err := h.State.AllocsByJob(ws, job.Namespace, job.ID, false)
	assert.Nil(err)

	assert.Len(out, 4)
	h.AssertEvalStatus(t, structs.EvalStatusComplete)
}

func TestServiceSched_QuotaInteractionsWithConstraints(t *testing.T) {

	require := require.New(t)
	h := NewHarness(t)

	// Create the quota spec (2000 cpu/mem)
	qs := mock.QuotaSpec()
	require.NoError(h.State.UpsertQuotaSpecs(h.NextIndex(), []*structs.QuotaSpec{qs}))

	// Create the namespace
	ns := mock.Namespace()
	ns.Quota = qs.Name
	require.NoError(h.State.UpsertNamespaces(h.NextIndex(), []*structs.Namespace{ns}))

	// Create a job with resources that allow for exactly one alloc
	job := mock.Job()
	job.Namespace = ns.Name
	job.TaskGroups[0].Count = 1
	job.TaskGroups[0].Constraints = []*structs.Constraint{
		{
			LTarget: "${node.class}",
			RTarget: "good",
			Operand: "=",
		},
	}
	job.TaskGroups[0].Tasks[0].Env = map[string]string{"example": "1"}
	r1 := job.TaskGroups[0].Tasks[0].Resources
	r1.CPU = 800
	r1.MemoryMB = 800
	r1.Networks = nil

	// quota usage should now be 800/2000
	require.NoError(h.State.UpsertJob(structs.MsgTypeTestSetup, h.NextIndex(), job))

	goodNode := mock.Node()
	goodNode.NodeClass = "good"
	goodNode.ComputeClass()
	require.NoError(h.State.UpsertNode(structs.MsgTypeTestSetup, h.NextIndex(), goodNode))

	goodNode2 := mock.Node()
	goodNode2.NodeClass = "good"
	goodNode2.ComputeClass()
	require.NoError(h.State.UpsertNode(structs.MsgTypeTestSetup, h.NextIndex(), goodNode2))

	// create a whole bunch of bad nodes to ensure we hit the failure case
	for i := 0; i < 100; i++ {
		badNode := mock.Node()
		badNode.NodeClass = "bad"
		badNode.ComputeClass()
		require.NoError(h.State.UpsertNode(structs.MsgTypeTestSetup, h.NextIndex(), badNode))
	}

	alloc := mock.Alloc()
	alloc.Job = job
	alloc.JobID = job.ID
	alloc.NodeID = goodNode.ID
	alloc.Namespace = ns.Name
	alloc.TaskGroup = job.TaskGroups[0].Name
	alloc.Name = fmt.Sprintf("%s.%s[0]", job.ID, alloc.TaskGroup)
	alloc.ClientStatus = structs.AllocClientStatusRunning
	alloc.Resources = r1.Copy()
	alloc.TaskResources = map[string]*structs.Resources{
		"web": r1.Copy(),
	}

	require.NoError(h.State.UpsertAllocs(
		structs.MsgTypeTestSetup, h.NextIndex(), []*structs.Allocation{alloc}))

	// Update the env to force an update
	job2 := job.Copy()
	job2.TaskGroups[0].Count = 2
	job2.TaskGroups[0].Tasks[0].Env = map[string]string{"example": "2"}
	require.NoError(h.State.UpsertJob(structs.MsgTypeTestSetup, h.NextIndex(), job2))

	eval := &structs.Evaluation{
		Namespace:   ns.Name,
		ID:          uuid.Generate(),
		Priority:    50,
		TriggeredBy: structs.EvalTriggerJobRegister,
		JobID:       job2.ID,
		Status:      structs.EvalStatusPending,
	}
	require.NoError(h.State.UpsertEvals(
		structs.MsgTypeTestSetup, h.NextIndex(), []*structs.Evaluation{eval}))

	// Process the evaluation
	require.NoError(h.Process(NewServiceScheduler, eval))

	quotaErrForEvals := func(evals []*structs.Evaluation) string {
		if len(evals) < 1 {
			return ""
		}
		if evals[0].QuotaLimitReached == "" {
			// should never see this unless we break the test itself
			return fmt.Sprintf("unexpected failure\n%#v", evals[0].FailedTGAllocs)
		}
		errMsg := "unexpected quota limit failure\n"
		errMsg += "-> " + evals[0].QuotaLimitReached
		for _, alloc := range evals[0].FailedTGAllocs {
			for _, exhausted := range alloc.QuotaExhausted {
				errMsg += fmt.Sprintf("\n-> %s", exhausted)
			}
		}
		return errMsg
	}

	// Ensure the plan has no failures or blocked evals
	require.Len(h.CreateEvals, 0, quotaErrForEvals(h.CreateEvals))
	require.Len(h.Evals, 1)
	h.AssertEvalStatus(t, structs.EvalStatusComplete)

	// Ensure a single plan that evicts the running alloc and gives us a new alloc
	require.Len(h.Plans, 1, "we should have exactly 1 plan")
	plan := h.Plans[0]
	require.Len(plan.NodeUpdate[alloc.NodeID], 1)

	// Ensure the plan allocated
	var planned []*structs.Allocation
	for _, allocList := range plan.NodeAllocation {
		planned = append(planned, allocList...)
	}
	require.Len(planned, 2)

}
