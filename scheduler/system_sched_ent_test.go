//go:build ent
// +build ent

package scheduler

import (
	"testing"

	memdb "github.com/hashicorp/go-memdb"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/shoenig/test/must"
)

// Tests registering a system job that will exceed the quota limit
func TestSystemSched_JobRegister_QuotaLimit(t *testing.T) {
	ci.Parallel(t)

	h := NewHarness(t)

	// Create the quota spec
	qs := mock.QuotaSpec()
	must.Nil(t, h.State.UpsertQuotaSpecs(h.NextIndex(), []*structs.QuotaSpec{qs}))

	// Create the namespace
	ns := mock.Namespace()
	ns.Quota = qs.Name
	must.Nil(t, h.State.UpsertNamespaces(h.NextIndex(), []*structs.Namespace{ns}))

	// Create the job
	job := mock.SystemJob()
	job.Namespace = ns.Name

	// Quota Limit: (2000 CPU, 2000 MB)
	// Should be able to place 4
	// Quota would be (2000, 1024)
	must.Nil(t, h.State.UpsertJob(structs.MsgTypeTestSetup, h.NextIndex(), nil, job))

	// Create several node
	var nodes []*structs.Node
	for i := 0; i < 10; i++ {
		nodes = append(nodes, mock.Node())
		must.Nil(t, h.State.UpsertNode(structs.MsgTypeTestSetup, h.NextIndex(), nodes[i]))
	}

	// Create a mock evaluation to deal with drain
	eval := &structs.Evaluation{
		Namespace:   ns.Name,
		ID:          uuid.Generate(),
		Priority:    50,
		TriggeredBy: structs.EvalTriggerJobRegister,
		JobID:       job.ID,
		Status:      structs.EvalStatusPending,
	}
	must.NoError(t, h.State.UpsertEvals(structs.MsgTypeTestSetup, h.NextIndex(), []*structs.Evaluation{eval}))

	// Process the evaluation
	must.Nil(t, h.Process(NewSystemScheduler, eval))

	// Ensure a single plan
	must.Len(t, 1, h.Plans)
	plan := h.Plans[0]

	// Ensure the plan didn't evicted the alloc
	var update []*structs.Allocation
	for _, updateList := range plan.NodeUpdate {
		update = append(update, updateList...)
	}
	must.SliceEmpty(t, update)

	// Ensure the plan allocated
	var planned []*structs.Allocation
	for _, allocList := range plan.NodeAllocation {
		planned = append(planned, allocList...)
	}
	must.Len(t, 4, planned)

	// Ensure the plan had a failures
	must.Len(t, 1, h.Evals)

	// XXX This should be checked once the system scheduler creates blocked
	// evals
	// Ensure the eval has spawned blocked eval
	//must.Len(h.CreateEvals, 1)

	// Lookup the allocations by JobID and make sure we have the right amount of
	// each type
	ws := memdb.NewWatchSet()
	out, err := h.State.AllocsByJob(ws, job.Namespace, job.ID, false)
	must.Nil(t, err)

	must.Len(t, 4, out)
	h.AssertEvalStatus(t, structs.EvalStatusComplete)
}
