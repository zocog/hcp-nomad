//go:build ent
// +build ent

package scheduler

import (
	"testing"

	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/stretchr/testify/assert"
)

func TestQuotaIterator_BelowQuota(t *testing.T) {
	assert := assert.New(t)
	state, ctx := testContext(t)

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

	// Create the source
	nodes := []*structs.Node{mock.Node()}
	static := NewStaticIterator(ctx, nodes)

	// Create the quota iterator
	quota := NewQuotaIterator(ctx, static)
	contextual := quota.(ContextualIterator)
	contextual.SetJob(job)
	contextual.SetTaskGroup(job.TaskGroups[0])
	quota.Reset()
	assert.Len(collectFeasible(quota), 1)
}

func TestQuotaIterator_AboveQuota(t *testing.T) {
	assert := assert.New(t)
	state, ctx := testContext(t)

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

	// Bump the resource usage
	job.TaskGroups[0].Tasks[0].Resources.CPU = 3000

	// Create the source
	nodes := []*structs.Node{mock.Node()}
	static := NewStaticIterator(ctx, nodes)

	// Create the quota iterator
	quota := NewQuotaIterator(ctx, static)
	contextual := quota.(ContextualIterator)
	contextual.SetJob(job)
	contextual.SetTaskGroup(job.TaskGroups[0])
	quota.Reset()
	assert.Len(collectFeasible(quota), 0)

	// Check that it marks the dimension that is exhausted
	assert.Len(ctx.Metrics().QuotaExhausted, 1)
	assert.Contains(ctx.Metrics().QuotaExhausted[0], "cpu")

	// Check it marks the quota limit being reached
	elig := ctx.Eligibility()
	assert.NotNil(elig)
	assert.Equal(qs.Name, elig.QuotaLimitReached())
}

func TestQuotaIterator_BelowQuota_PlannedAdditions(t *testing.T) {
	assert := assert.New(t)
	state, ctx := testContext(t)

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

	// Create the source
	nodes := []*structs.Node{mock.Node()}
	static := NewStaticIterator(ctx, nodes)

	// Add a planned alloc to node1 that is still below quota
	plan := ctx.Plan()
	plan.NodeAllocation[nodes[0].ID] = []*structs.Allocation{mock.Alloc()}

	// Create the quota iterator
	quota := NewQuotaIterator(ctx, static)
	contextual := quota.(ContextualIterator)
	contextual.SetJob(job)
	contextual.SetTaskGroup(job.TaskGroups[0])
	quota.Reset()
	assert.Len(collectFeasible(quota), 1)
}

func TestQuotaIterator_AboveQuota_PlannedAdditions(t *testing.T) {
	assert := assert.New(t)
	state, ctx := testContext(t)

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

	// Create the source
	nodes := []*structs.Node{mock.Node()}
	static := NewStaticIterator(ctx, nodes)

	// Add a planned alloc to node1 that fills the quota
	plan := ctx.Plan()
	plan.NodeAllocation[nodes[0].ID] = []*structs.Allocation{
		mock.Alloc(),
		mock.Alloc(),
		mock.Alloc(),
		mock.Alloc(),
	}

	// Create the quota iterator
	quota := NewQuotaIterator(ctx, static)
	contextual := quota.(ContextualIterator)
	contextual.SetJob(job)
	contextual.SetTaskGroup(job.TaskGroups[0])
	quota.Reset()
	assert.Len(collectFeasible(quota), 0)

	// Check it marks the quota limit being reached
	elig := ctx.Eligibility()
	assert.NotNil(elig)
	assert.Equal(qs.Name, elig.QuotaLimitReached())
}

func TestQuotaIterator_BelowQuota_DiscountStopping(t *testing.T) {
	assert := assert.New(t)
	state, ctx := testContext(t)

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

	// Create the source
	nodes := []*structs.Node{mock.Node()}
	static := NewStaticIterator(ctx, nodes)

	// Add a planned alloc to node1 that fills the quota
	plan := ctx.Plan()
	plan.NodeAllocation[nodes[0].ID] = []*structs.Allocation{
		mock.Alloc(),
		mock.Alloc(),
		mock.Alloc(),
		mock.Alloc(),
	}

	// Add a planned eviction that makes it possible however to still place
	evict := mock.Alloc()
	evict.DesiredStatus = structs.AllocDesiredStatusEvict
	plan.NodeUpdate[nodes[0].ID] = []*structs.Allocation{evict}

	// Create the quota iterator
	quota := NewQuotaIterator(ctx, static)
	contextual := quota.(ContextualIterator)
	contextual.SetJob(job)
	contextual.SetTaskGroup(job.TaskGroups[0])
	quota.Reset()
	assert.Len(collectFeasible(quota), 1)
}

// TestQuotaIterator_BelowQuota_MultipleNextCallsForAlloc ensures the proposed
// limit of the QuotaIterator is not modified until we have placed an
// allocation.
//
// This specifically tests against a found bug which was caused by the iterator
// modifying the proposedLimit even if the node was later deemed not suitable
// for placing the allocation on.
func TestQuotaIterator_BelowQuota_MultipleNextCallsForAlloc(t *testing.T) {
	newAssert := assert.New(t)
	state, ctx := testContext(t)

	// Create a quota spec which has enough resource limit to cover over 50% of
	// that needed by the mock job. The values at the time of writing are 1.6
	// times that required.
	qs := &structs.QuotaSpec{
		Name:        "quota-spec-" + uuid.Generate(),
		Description: "Super uncool quota!",
		Limits: []*structs.QuotaLimit{
			{
				Region: "global",
				RegionLimit: &structs.Resources{
					CPU:      8000,
					MemoryMB: 4096,
				},
			},
		},
	}
	qs.SetHash()

	newAssert.Nil(state.UpsertQuotaSpecs(100, []*structs.QuotaSpec{qs}))

	// Create the namespace and ensure the quota is linked.
	ns := mock.Namespace()
	ns.Quota = qs.Name
	newAssert.Nil(state.UpsertNamespaces(200, []*structs.Namespace{ns}))

	// Create the job.
	job := mock.Job()
	job.Namespace = ns.Name

	// Create the source with three nodes, so we can hit the Next() function
	// a couple of times.
	nodes := make([]*structs.Node, 3)

	for i := 0; i < 3; i++ {
		nodes[i] = mock.Node()
	}
	static := NewStaticIterator(ctx, nodes)

	// Create the quota iterator using the struct rather than the factory
	// method, so we have access to its glorious internals.
	quota := &QuotaIterator{ctx: ctx, source: static}

	// Perform the calls performed when starting a new placement calculation.
	quota.SetJob(job)
	quota.SetTaskGroup(job.TaskGroups[0])
	quota.Reset()

	// Copy the current proposed limit state. Until the context is updated with
	// a NodeUpdate or NodeAllocation and Reset is called, this should not
	// change.
	currentLimitState := quota.proposedLimit.Copy()

	// Iterate through nodes in the stack, each time checking that the proposed
	// limit has not changed.
	newAssert.Equal(currentLimitState, quota.proposedLimit)
	_ = quota.Next()
	newAssert.Equal(currentLimitState, quota.proposedLimit)
	_ = quota.Next()
	newAssert.Equal(currentLimitState, quota.proposedLimit)
}
