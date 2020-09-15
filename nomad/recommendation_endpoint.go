// +build ent

package nomad

import (
	"fmt"
	"time"

	metrics "github.com/armon/go-metrics"
	"github.com/hashicorp/go-memdb"
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
	args.Region = r.srv.config.AuthoritativeRegion
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
		run: func(ws memdb.WatchSet, state *state.StateStore) error {
			var out []*structs.Recommendation
			var err error
			if args.JobID == "" {
				out, err = state.RecommendationsByNamespace(ws, args.Namespace)
			} else {
				out, err = state.RecommendationsByJob(ws, args.Namespace, args.JobID, args.Group, args.Task)
			}
			if err != nil {
				return err
			}

			reply.Recommendations = out
			// use the last index that affected the recommendations table
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
	var group *structs.TaskGroup
	for _, g := range job.TaskGroups {
		if g.Name == args.Recommendation.Group {
			group = g
			break
		}
	}
	if group == nil {
		return structs.NewErrRPCCoded(400, fmt.Sprintf("group %q does not exist in job", args.Recommendation.Group))
	}
	var task *structs.Task
	for _, t := range group.Tasks {
		if t.Name == args.Recommendation.Task {
			task = t
			break
		}
	}
	if task == nil {
		return structs.NewErrRPCCoded(400, fmt.Sprintf("task %q does not exist in group %q", args.Recommendation.Task, args.Recommendation.Group))
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
	var outRec *structs.Recommendation
	jobRecs, err := r.srv.fsm.State().RecommendationsByJob(nil, job.Namespace, job.ID, "", "")
	if err != nil {
		return fmt.Errorf("recommendation lookup fail: %v", err)
	}
	for _, r := range jobRecs {
		if r.SamePath(args.Recommendation) {
			outRec = r
			break
		}
	}

	if outRec == nil {
		return fmt.Errorf("could not find recommendation by path after log apply")
	}

	// Update the index
	reply.Index = index
	reply.Recommendation = outRec

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
			return structs.NewErrRPCCoded(400, fmt.Sprintf("recommendation %q does not exist", id))
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

// allowedRecNSes returns a set (as map of ns->true) of the namespaces a token
// has access to, for the purpose of reading recommendations.
// Returns `nil` set if the token has access to all namespaces
// and ErrPermissionDenied if the token has no capabilities on any namespace.
func allowedRecNSes(aclObj *acl.ACL, state *state.StateStore) (map[string]bool, error) {
	if aclObj == nil || aclObj.IsManagement() {
		return nil, nil
	}

	// namespaces
	nses, err := state.NamespaceNames()
	if err != nil {
		return nil, err
	}

	r := make(map[string]bool, len(nses))

	for _, ns := range nses {
		if aclObj.AllowNsOp(ns, acl.NamespaceCapabilityReadJob) ||
			aclObj.AllowNsOp(ns, acl.NamespaceCapabilitySubmitRecommendation) {
			r[ns] = true
		}
	}

	if len(r) == 0 {
		return nil, structs.ErrPermissionDenied
	}

	return r, nil
}

func (r *Recommendation) listAllRecommendations(args *structs.RecommendationListRequest, reply *structs.RecommendationListResponse) error {
	aclObj, err := r.srv.ResolveToken(args.AuthToken)
	if err != nil {
		return err
	}

	// Setup the blocking query
	opts := blockingOptions{
		queryOpts: &args.QueryOptions,
		queryMeta: &reply.QueryMeta,
		run: func(ws memdb.WatchSet, state *state.StateStore) error {
			// check if user has permission to all namespaces
			allowedNSes, err := allowedRecNSes(aclObj, state)
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
				if allowedNSes != nil && !allowedNSes[rec.Namespace] {
					// not permitted to this name namespace
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
