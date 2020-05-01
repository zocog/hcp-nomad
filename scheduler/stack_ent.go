// +build ent

package scheduler

import (
	"fmt"

	"github.com/hashicorp/nomad/nomad/structs"
)

// NewGenericStack constructs a stack used for selecting service placements
func NewGenericStack(batch bool, ctx Context) *GenericStack {
	// Create a new stack
	s := &GenericStack{
		batch: batch,
		ctx:   ctx,
	}

	// Create the source iterator. We randomize the order we visit nodes
	// to reduce collisions between schedulers and to do a basic load
	// balancing across eligible nodes.
	s.source = NewRandomIterator(ctx, nil)

	// Create the quota iterator to determine if placements would result in the
	// quota attached to the namespace of the job to go over.
	s.quota = NewQuotaIterator(ctx, s.source)

	// Attach the job constraints. The job is filled in later.
	s.jobConstraint = NewConstraintChecker(ctx, nil)

	// Filter on task group drivers first as they are faster
	s.taskGroupDrivers = NewDriverChecker(ctx, nil)

	// Filter on task group constraints second
	s.taskGroupConstraint = NewConstraintChecker(ctx, nil)

	// Filter on task group devices
	s.taskGroupDevices = NewDeviceChecker(ctx)

	// Filter on task group host volumes
	s.taskGroupHostVolumes = NewHostVolumeChecker(ctx)

	// Filter on available, healthy CSI plugins
	s.taskGroupCSIVolumes = NewCSIVolumeChecker(ctx)

	// Create the feasibility wrapper which wraps all feasibility checks in
	// which feasibility checking can be skipped if the computed node class has
	// previously been marked as eligible or ineligible. Generally this will be
	// checks that only needs to examine the single node to determine feasibility.
	jobs := []FeasibilityChecker{s.jobConstraint}
	tgs := []FeasibilityChecker{s.taskGroupDrivers,
		s.taskGroupConstraint,
		s.taskGroupHostVolumes,
		s.taskGroupDevices}
	avail := []FeasibilityChecker{s.taskGroupCSIVolumes}
	s.wrappedChecks = NewFeasibilityWrapper(ctx, s.quota, jobs, tgs, avail)

	// Filter on distinct host constraints.
	s.distinctHostsConstraint = NewDistinctHostsIterator(ctx, s.wrappedChecks)

	// Filter on distinct property constraints.
	s.distinctPropertyConstraint = NewDistinctPropertyIterator(ctx, s.distinctHostsConstraint)

	// Upgrade from feasible to rank iterator
	rankSource := NewFeasibleRankIterator(ctx, s.distinctPropertyConstraint)

	// Apply the bin packing, this depends on the resources needed
	// by a particular task group.
	_, schedConfig, _ := ctx.State().SchedulerConfig()
	s.binPack = NewBinPackIterator(ctx, rankSource, false, 0, schedConfig.EffectiveSchedulerAlgorithm())

	// Apply the job anti-affinity iterator. This is to avoid placing
	// multiple allocations on the same node for this job.
	s.jobAntiAff = NewJobAntiAffinityIterator(ctx, s.binPack, "")

	// Apply node rescheduling penalty. This tries to avoid placing on a
	// node where the allocation failed previously
	s.nodeReschedulingPenalty = NewNodeReschedulingPenaltyIterator(ctx, s.jobAntiAff)

	// Apply scores based on affinity stanza
	s.nodeAffinity = NewNodeAffinityIterator(ctx, s.nodeReschedulingPenalty)

	// Apply scores based on spread stanza
	s.spread = NewSpreadIterator(ctx, s.nodeAffinity)

	// Add the preemption options scoring iterator
	preemptionScorer := NewPreemptionScoringIterator(ctx, s.spread)

	// Normalizes scores by averaging them across various scorers
	s.scoreNorm = NewScoreNormalizationIterator(ctx, preemptionScorer)

	// Apply a limit function. This is to avoid scanning *every* possible node.
	s.limit = NewLimitIterator(ctx, s.scoreNorm, 2, skipScoreThreshold, maxSkip)

	// Select the node with the maximum score for placement
	s.maxScore = NewMaxScoreIterator(ctx, s.limit)
	return s
}

// QuotaIterator is used to enforce resource quotas. When below the quota limit,
// the iterator acts as a pass through and above it will deny all nodes
type QuotaIterator struct {
	ctx           Context
	source        FeasibleIterator
	buildErr      error
	tg            *structs.TaskGroup
	job           *structs.Job
	quota         *structs.QuotaSpec
	quotaLimits   map[string]*structs.QuotaLimit
	actUsage      *structs.QuotaUsage
	proposedUsage *structs.QuotaUsage

	// proposedLimit is the limit that applies to this job. At this point there
	// can only be a single quota limit per region so there can only be one.
	proposedLimit *structs.QuotaLimit
}

// NewQuotaIterator returns a new quota iterator reading from the passed source.
func NewQuotaIterator(ctx Context, source FeasibleIterator) FeasibleIterator {
	return &QuotaIterator{
		ctx:    ctx,
		source: source,
	}
}

func (iter *QuotaIterator) SetTaskGroup(tg *structs.TaskGroup) {
	iter.tg = tg
}

func (iter *QuotaIterator) SetJob(job *structs.Job) {
	iter.job = job

	// Get the converted state object
	state := iter.ctx.State().(StateEnterprise)
	namespace, err := state.NamespaceByName(nil, job.Namespace)
	if err != nil {
		iter.buildErr = fmt.Errorf("failed to lookup job %q namespace %q: %v", job.ID, job.Namespace, err)
		iter.ctx.Logger().Named("stack").Error("scheduler.QuotaIterator", "error", iter.buildErr)
		return
	} else if namespace == nil {
		iter.buildErr = fmt.Errorf("unknown namespace %q referenced by job %q", job.Namespace, job.ID)
		iter.ctx.Logger().Named("stack").Error("scheduler.QuotaIterator ", "error", iter.buildErr)
		return
	}

	// There is no quota attached to the namespace so there is nothing for the
	// iterator to do
	if namespace.Quota == "" {
		return
	}

	// Lookup the quota spec
	quota, err := state.QuotaSpecByName(nil, namespace.Quota)
	if err != nil {
		iter.buildErr = fmt.Errorf("failed to lookup quota %q: %v", namespace.Quota, err)
		iter.ctx.Logger().Named("stack").Error("scheduler.QuotaIterator", "error", iter.buildErr)
		return
	} else if quota == nil {
		iter.buildErr = fmt.Errorf("unknown quota %q referenced by namespace %q", namespace.Quota, namespace.Name)
		iter.ctx.Logger().Named("stack").Error("scheduler.QuotaIterator", "error", iter.buildErr)
		return
	}

	// Lookup the current quota usage
	usage, err := state.QuotaUsageByName(nil, namespace.Quota)
	if err != nil {
		iter.buildErr = fmt.Errorf("failed to lookup quota usage %q: %v", namespace.Quota, err)
		iter.ctx.Logger().Named("stack").Error("scheduler.QuotaIterator", "error", iter.buildErr)
		return
	} else if usage == nil {
		iter.buildErr = fmt.Errorf("unknown quota usage %q", namespace.Quota)
		iter.ctx.Logger().Named("stack").Error("scheduler.QuotaIterator", "error", iter.buildErr)
		return
	}

	// There is no limit that applies to us
	if len(usage.Used) == 0 {
		return
	}

	// Store the quota and usage since it applies to us
	iter.quota = quota
	iter.quotaLimits = quota.LimitsMap()
	iter.actUsage = usage
}

func (iter *QuotaIterator) Next() *structs.Node {
	// Get the next option from the source
	option := iter.source.Next()

	// If there is no quota or there was an error building the iterator so
	// just act as a pass through.
	if option == nil || iter.quota == nil || iter.buildErr != nil {
		return option
	}

	// Add the resources of the proposed task group
	iter.proposedLimit.AddResource(combinedResources(iter.tg))

	// Get the actual limit
	quotaLimit := iter.quotaLimits[string(iter.proposedLimit.Hash)]

	superset, dimensions := quotaLimit.Superset(iter.proposedLimit)
	if superset {
		return option
	}

	// Mark the dimensions that caused the quota to be exhausted
	iter.ctx.Metrics().ExhaustQuota(dimensions)

	// Store the fact that the option was rejected because the quota limit was
	// reached.
	iter.ctx.Eligibility().SetQuotaLimitReached(iter.quota.Name)

	return nil
}

func (iter *QuotaIterator) Reset() {
	iter.source.Reset()

	// There is nothing more to do
	if iter.quota == nil {
		return
	}

	// Populate the quota usage with proposed allocations
	iter.proposedUsage = iter.actUsage.Copy()
	structs.UpdateUsageFromPlan(iter.proposedUsage, iter.ctx.Plan())

	// At this point there will be only one limit and it will apply.
	for _, l := range iter.proposedUsage.Used {
		iter.proposedLimit = l
	}
}

// combinedResources returns the combined resources for the task group
func combinedResources(tg *structs.TaskGroup) *structs.Resources {
	r := &structs.Resources{
		DiskMB: tg.EphemeralDisk.SizeMB,
	}
	for _, task := range tg.Tasks {
		r.Add(task.Resources)
	}
	return r
}
