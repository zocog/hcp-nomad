//go:build ent
// +build ent

package nomad

import (
	"fmt"
	"strings"
	"time"

	metrics "github.com/armon/go-metrics"
	"github.com/hashicorp/go-memdb"
	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/nomad-licensing/license"

	"github.com/hashicorp/nomad/acl"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"
)

// Recommendation endpoint is used for manipulating namespaces
type Recommendation struct {
	srv *Server
}

// GetRecommendation is used to query a recommendation.
func (r *Recommendation) GetRecommendation(args *structs.RecommendationSpecificRequest,
	reply *structs.SingleRecommendationResponse) error {
	if done, err := r.srv.forward("Recommendation.GetRecommendation", args, args, reply); done {
		return err
	}
	defer metrics.MeasureSince([]string{"nomad", "recommendation", "get_recommendation"}, time.Now())

	allowNsOp := acl.NamespaceValidator(acl.NamespaceCapabilityReadJob)
	aclObj, err := r.srv.ResolveToken(args.AuthToken)
	if err != nil {
		return err
	} else if aclObj != nil && !allowNsOp(aclObj, args.RequestNamespace()) {
		return structs.ErrPermissionDenied
	}

	// Strict enforcement for read requests - if not licensed then requests will be denied
	if err := r.srv.EnterpriseState.FeatureCheck(license.FeatureDynamicApplicationSizing, true); err != nil {
		return err
	}

	// Verify the arguments
	if args.RecommendationID == "" {
		return fmt.Errorf("missing recommendation ID")
	}

	// Setup the blocking query
	opts := blockingOptions{
		queryOpts: &args.QueryOptions,
		queryMeta: &reply.QueryMeta,
		run: func(ws memdb.WatchSet, state *state.StateStore) error {

			out, err := state.RecommendationByID(ws, args.RecommendationID)
			if err != nil {
				return err
			}
			if out != nil && !allowNsOp(aclObj, out.Namespace) {
				// we did a preliminary check against the request namespace, check against the actual namespace
				out = nil
			}

			reply.Recommendation = out
			if out != nil {
				reply.Index = out.ModifyIndex
			} else {
				index, err := state.Index("recommendations")
				if err != nil {
					return err
				}
				reply.Index = index
			}

			// Set the query response
			r.srv.setQueryMeta(&reply.QueryMeta)
			return nil
		}}
	return r.srv.blockingRPC(&opts)
}

// ListRecommendations is used to retrieve of a list of recommendations in a namespace
func (r *Recommendation) ListRecommendations(args *structs.RecommendationListRequest,
	reply *structs.RecommendationListResponse) error {
	if done, err := r.srv.forward("Recommendation.ListRecommendations", args, args, reply); done {
		return err
	}
	defer metrics.MeasureSince([]string{"nomad", "recommendation", "list_recommendations"}, time.Now())

	// Strict enforcement for read requests - if not licensed then requests will be denied
	if err := r.srv.EnterpriseState.FeatureCheck(license.FeatureDynamicApplicationSizing, true); err != nil {
		return err
	}

	if args.RequestNamespace() == structs.AllNamespacesSentinel {
		return r.listAllRecommendations(args, reply)
	}

	allowNsOp := acl.NamespaceValidator(acl.NamespaceCapabilityReadJob,
		acl.NamespaceCapabilitySubmitRecommendation, acl.NamespaceCapabilitySubmitJob)
	aclObj, err := r.srv.ResolveToken(args.AuthToken)
	if err != nil {
		return err
	} else if aclObj != nil && !allowNsOp(aclObj, args.RequestNamespace()) {
		return structs.ErrPermissionDenied
	}

	// Setup the blocking query
	opts := blockingOptions{
		queryOpts: &args.QueryOptions,
		queryMeta: &reply.QueryMeta,
		run: func(ws memdb.WatchSet, store *state.StateStore) error {
			var out []*structs.Recommendation
			var err error
			if prefix := args.QueryOptions.Prefix; prefix != "" {
				out, err = store.RecommendationsByIDPrefix(ws, args.RequestNamespace(), prefix)
			} else if args.JobID == "" {
				out, err = store.RecommendationsByNamespace(ws, args.Namespace)
			} else {
				out, err = store.RecommendationsByJob(ws, args.Namespace, args.JobID, &state.RecommendationsFilter{
					Group: args.Group,
					Task:  args.Task,
				})
			}
			if err != nil {
				return err
			}

			reply.Recommendations = out
			// use the last index that affected the recommendations table
			index, err := store.Index("recommendations")
			if err != nil {
				return err
			}
			reply.Index = index

			// Set the query response
			r.srv.setQueryMeta(&reply.QueryMeta)
			return nil
		}}
	return r.srv.blockingRPC(&opts)
}

// UpsertRecommendation is used to upsert a recommendation
func (r *Recommendation) UpsertRecommendation(args *structs.RecommendationUpsertRequest,
	reply *structs.SingleRecommendationResponse) error {
	if done, err := r.srv.forward("Recommendation.UpsertRecommendation", args, args, reply); done {
		return err
	}
	defer metrics.MeasureSince([]string{"nomad", "recommendation", "upsert_recommendation"}, time.Now())

	// determine namespace before ACL check
	if args.Namespace == "" {
		args.Namespace = args.Recommendation.Namespace
	} else if args.Recommendation.Namespace == "" {
		args.Recommendation.Namespace = args.RequestNamespace()
	} else if args.Recommendation.Namespace != args.RequestNamespace() {
		return structs.NewErrRPCCoded(400, "mismatched request namespace")
	}

	// Check submit-recommendation permissions
	allowNsOp := acl.NamespaceValidator(acl.NamespaceCapabilitySubmitRecommendation, acl.NamespaceCapabilitySubmitJob)
	aclObj, err := r.srv.ResolveToken(args.AuthToken)
	if err != nil {
		return err
	} else if aclObj != nil && !allowNsOp(aclObj, args.RequestNamespace()) {
		return structs.ErrPermissionDenied
	}

	// Strict enforcement for write requests - if not licensed then requests will be denied
	if err := r.srv.EnterpriseState.FeatureCheck(license.FeatureDynamicApplicationSizing, true); err != nil {
		return err
	}

	// Validate
	err = args.Recommendation.Validate()
	if err != nil {
		return fmt.Errorf("invalid recommendation: %v", err)
	}

	// Check whether the associated job exists
	snap, err := r.srv.fsm.State().Snapshot()
	if err != nil {
		return fmt.Errorf("error creating snapshot: %v", err)
	}
	job, err := snap.JobByID(nil, args.Recommendation.Namespace, args.Recommendation.JobID)
	if err != nil {
		return fmt.Errorf("error looking up job: %v", err)
	}
	if job == nil {
		return structs.NewErrRPCCoded(400, fmt.Sprintf("job %q does not exist in namespace %q",
			args.Recommendation.JobID, args.Recommendation.Namespace))
	}

	// Validate the path against the job
	group := job.LookupTaskGroup(args.Recommendation.Group)
	if group == nil {
		return structs.NewErrRPCCoded(400, fmt.Sprintf("group %q does not exist in job", args.Recommendation.Group))
	}
	task := group.LookupTask(args.Recommendation.Task)
	if task == nil {
		return structs.NewErrRPCCoded(400, fmt.Sprintf("task %q does not exist in group %q", args.Recommendation.Task, args.Recommendation.Group))
	}
	switch args.Recommendation.Resource {
	case "CPU":
		args.Recommendation.Current = task.Resources.CPU
	case "MemoryMB":
		args.Recommendation.Current = task.Resources.MemoryMB
	}
	args.Recommendation.JobVersion = job.Version

	// Check whether the recommendation exists by provided ID
	var existing *structs.Recommendation
	if args.Recommendation.ID != "" {
		var err error
		existing, err = snap.RecommendationByID(nil, args.Recommendation.ID)
		if err != nil {
			return fmt.Errorf("error looking up recommendation: %v", err)
		}
	}
	if existing != nil && !existing.SamePath(args.Recommendation) {
		return structs.NewErrRPCCoded(400, "cannot update recommendation path")
	}

	if existing != nil {
		args.Recommendation.ID = existing.ID
	} else {
		// don't let callers specify UUIDs for now
		args.Recommendation.ID = uuid.Generate()
	}
	args.Recommendation.SubmitTime = time.Now().UnixNano()

	// Update via Raft
	out, index, err := r.srv.raftApply(structs.RecommendationUpsertRequestType, args)
	if err != nil {
		return err
	}

	// Check if there was an error when applying.
	if err, ok := out.(error); ok && err != nil {
		return err
	}

	// the FSM will only allow one recommendation per path
	// find that recommendation and return it
	jobRecs, err := r.srv.fsm.State().RecommendationsByJob(nil, job.Namespace, job.ID, &state.RecommendationsFilter{
		Group:    args.Recommendation.Group,
		Task:     args.Recommendation.Task,
		Resource: args.Recommendation.Resource,
	})
	if err != nil {
		return fmt.Errorf("recommendation lookup fail: %v", err)
	}
	if len(jobRecs) != 1 {
		return fmt.Errorf("could not find recommendation by path after log apply")

	}

	// Update the index
	reply.Index = index
	reply.Recommendation = jobRecs[0]

	return nil
}

// DeleteRecommendations is used to delete one or more recommendations
func (r *Recommendation) DeleteRecommendations(args *structs.RecommendationDeleteRequest,
	reply *structs.GenericResponse) error {
	if done, err := r.srv.forward("Recommendation.DeleteRecommendations", args, args, reply); done {
		return err
	}
	defer metrics.MeasureSince([]string{"nomad", "namespace", "delete_recommendations"}, time.Now())

	// Check submit-recommendation permissions
	allowNsOp := acl.NamespaceValidator(acl.NamespaceCapabilitySubmitRecommendation, acl.NamespaceCapabilitySubmitJob)
	aclObj, err := r.srv.ResolveToken(args.AuthToken)
	if err != nil {
		return err
	}

	// Strict enforcement for delete requests - if not licensed then requests will be denied
	if err := r.srv.EnterpriseState.FeatureCheck(license.FeatureDynamicApplicationSizing, true); err != nil {
		return err
	}

	// Validate: at least one recommendation
	if len(args.Recommendations) == 0 {
		return fmt.Errorf("must specify at least one recommendation to delete")
	}

	// Check whether the recommendations exist and whether we have permission on their namespace
	snap, err := r.srv.fsm.State().Snapshot()
	for _, id := range args.Recommendations {
		rec, err := snap.RecommendationByID(nil, id)
		if err != nil {
			return fmt.Errorf("error getting recommendations")
		}
		if rec == nil {
			return structs.NewErrRPCCoded(404, fmt.Sprintf("recommendation %q does not exist", id))
		}
		if aclObj != nil && !allowNsOp(aclObj, rec.Namespace) {
			return structs.ErrPermissionDenied
		}
	}

	// Update via Raft
	out, index, err := r.srv.raftApply(structs.RecommendationDeleteRequestType, args)
	if err != nil {
		return err
	}

	// Check if there was an error when applying.
	if err, ok := out.(error); ok && err != nil {
		return err
	}

	// Update the index
	reply.Index = index
	return nil
}

func (r *Recommendation) listAllRecommendations(args *structs.RecommendationListRequest, reply *structs.RecommendationListResponse) error {
	aclObj, err := r.srv.ResolveToken(args.AuthToken)
	if err != nil {
		return err
	}
	prefix := args.QueryOptions.Prefix
	allow := func(ns string) bool {
		return aclObj.AllowNsOp(ns, acl.NamespaceCapabilityReadJob) ||
			aclObj.AllowNsOp(ns, acl.NamespaceCapabilitySubmitRecommendation)
	}

	// Setup the blocking query
	opts := blockingOptions{
		queryOpts: &args.QueryOptions,
		queryMeta: &reply.QueryMeta,
		run: func(ws memdb.WatchSet, state *state.StateStore) error {
			// check if user has permission to all namespaces
			allowedNSes, err := allowedNSes(aclObj, state, allow)
			if err == structs.ErrPermissionDenied {
				// return empty list if token isn't authorized for any
				// namespace, matching other endpoints
				reply.Recommendations = []*structs.Recommendation{}
				return nil
			} else if err != nil {
				return err
			}

			// Capture all the recommendations
			iter, err := state.Recommendations(ws)
			if err != nil {
				return err
			}

			recs := []*structs.Recommendation{}
			for {
				raw := iter.Next()
				if raw == nil {
					break
				}
				rec := raw.(*structs.Recommendation)
				if prefix != "" && !strings.HasPrefix(rec.ID, prefix) {
					continue
				}
				if allowedNSes != nil && !allowedNSes[rec.Namespace] {
					// not permitted to this namespace
					continue
				}
				recs = append(recs, rec)
			}
			reply.Recommendations = recs

			// Use the last index that affected the jobs table or summary
			index, err := state.Index("recommendations")
			if err != nil {
				return err
			}
			reply.Index = index

			// Set the query response
			r.srv.setQueryMeta(&reply.QueryMeta)
			return nil
		}}
	return r.srv.blockingRPC(&opts)
}

// ApplyRecommendations is used to apply some number of recommendations by updating
// the associated jobs.
func (r *Recommendation) ApplyRecommendations(args *structs.RecommendationApplyRequest,
	reply *structs.RecommendationApplyResponse) error {
	if done, err := r.srv.forward("Recommendation.ApplyRecommendations", args, args, reply); done {
		return err
	}
	defer metrics.MeasureSince([]string{"nomad", "recommendation", "apply_recommendations"}, time.Now())

	// Prepare for ACL checking, but this will happen below when we have resolved the recommendations
	// and their individual namespaces
	readRecOp := acl.NamespaceValidator(acl.NamespaceCapabilityReadJob,
		acl.NamespaceCapabilitySubmitJob, acl.NamespaceCapabilitySubmitRecommendation)
	submitJobOp := acl.NamespaceValidator(acl.NamespaceCapabilitySubmitJob)
	aclObj, err := r.srv.ResolveToken(args.AuthToken)
	if err != nil {
		return err
	}

	// Strict enforcement for apply operations - if not licensed then requests will be denied
	if err := r.srv.EnterpriseState.FeatureCheck(license.FeatureDynamicApplicationSizing, true); err != nil {
		return err
	}

	// Validate
	if len(args.Recommendations) == 0 {
		return structs.NewErrRPCCoded(400, "must provide at least one recommendation")
	}

	// Collect recommendations and jobs
	snap, err := r.srv.fsm.State().Snapshot()
	if err != nil {
		return fmt.Errorf("error creating snapshot: %v", err)
	}
	var mErr multierror.Error
	recs := make([]*structs.Recommendation, 0, len(args.Recommendations))
	for _, id := range args.Recommendations {
		rec, err := snap.RecommendationByID(nil, id)
		if err != nil {
			return err
		}
		if rec == nil || (aclObj != nil && !readRecOp(aclObj, rec.Namespace)) {
			// doesn't exist (absolutely or relative to our token)
			mErr.Errors = append(mErr.Errors, fmt.Errorf("recommendation does not exist: %v", id))
		} else if aclObj != nil && !submitJobOp(aclObj, rec.Namespace) {
			// catch this early: don't have permission to apply this recommendation
			return structs.ErrPermissionDenied
		} else {
			recs = append(recs, rec)
		}
	}
	if len(mErr.Errors) > 0 {
		return structs.NewErrRPCCoded(400, mErr.ErrorOrNil().Error())
	}

	// Get the associated jobs and update
	type jobKey struct {
		ns  string
		job string
	}
	jobUpdates := map[jobKey]*structs.JobRegisterRequest{}
	recsByJob := map[jobKey][]string{}
	for _, rec := range recs {
		k := jobKey{
			ns:  rec.Namespace,
			job: rec.JobID,
		}
		recsByJob[k] = append(recsByJob[k], rec.ID)
		update := jobUpdates[k]
		if update == nil {
			job, err := snap.JobByID(nil, k.ns, k.job)
			if err != nil {
				return fmt.Errorf("error looking up job %q in namespace %q", k.job, k.ns)
			}
			if job == nil {
				return fmt.Errorf("job %q in namespace %q does not exist", k.job, k.ns)
			}
			// must work on a copy of the job, not the entry in the state store
			job = job.Copy()
			job.Version++
			update = &structs.JobRegisterRequest{
				Job:            job,
				EnforceIndex:   true,
				JobModifyIndex: job.JobModifyIndex,
				PreserveCounts: false,
				PolicyOverride: args.PolicyOverride,
				WriteRequest: structs.WriteRequest{
					Region:    args.RequestRegion(),
					Namespace: job.Namespace,
					AuthToken: args.AuthToken,
				},
			}
			jobUpdates[k] = update
		}
		// apply the recommendation change to the job
		if err := rec.UpdateJob(update.Job); err != nil {
			return err
		}
	}

	// the job updates below may invalidate the recommendations, so we need to delete them first
	var delResp structs.GenericResponse
	if err := r.DeleteRecommendations(&structs.RecommendationDeleteRequest{
		Recommendations: args.Recommendations,
		WriteRequest: structs.WriteRequest{
			Region:    args.RequestRegion(),
			AuthToken: args.AuthToken,
		}}, &delResp); err != nil {
		return fmt.Errorf("error deleting recommendations before apply: %v", err.Error())
	}

	index := delResp.Index
	for _, update := range jobUpdates {
		var updateResp structs.JobRegisterResponse
		if err := r.srv.RPC("Job.Register", update, &updateResp); err != nil {
			reply.Errors = append(reply.Errors, &structs.SingleRecommendationApplyError{
				Namespace: update.Namespace,
				JobID:     update.Job.ID,
				Recommendations: recsByJob[jobKey{
					ns:  update.Namespace,
					job: update.Job.ID,
				}],
				Error: err.Error(),
			})
		} else {
			reply.UpdatedJobs = append(reply.UpdatedJobs, &structs.SingleRecommendationApplyResult{
				Namespace:       update.Namespace,
				JobID:           update.Job.ID,
				JobModifyIndex:  updateResp.JobModifyIndex,
				EvalID:          updateResp.EvalID,
				EvalCreateIndex: updateResp.EvalCreateIndex,
				Warnings:        updateResp.Warnings,
				Recommendations: recsByJob[jobKey{
					ns:  update.Namespace,
					job: update.Job.ID,
				}],
			})
			index = updateResp.JobModifyIndex
			if updateResp.EvalCreateIndex > index {
				index = updateResp.EvalCreateIndex
			}
		}
	}

	reply.Index = index
	return nil
}
