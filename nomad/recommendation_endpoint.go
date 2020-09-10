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

	// Check for read-job permissions
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
			if out != nil && !allowNsOp(aclObj, out.JobNamespace) {
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

// UpsertRecommendation is used to upsert a recommendation
func (r *Recommendation) UpsertRecommendation(args *structs.RecommendationUpsertRequest,
	reply *structs.SingleRecommendationResponse) error {
	if done, err := r.srv.forward("Recommendation.UpsertRecommendation", args, args, reply); done {
		return err
	}
	defer metrics.MeasureSince([]string{"nomad", "recommendation", "upsert_recommendation"}, time.Now())

	// determine namespace before ACL check
	if args.Namespace == "" {
		args.Namespace = args.Recommendation.JobNamespace
	} else if args.Recommendation.JobNamespace == "" {
		args.Recommendation.JobNamespace = args.RequestNamespace()
	} else if args.Recommendation.JobNamespace != args.RequestNamespace() {
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
	path, err := args.Recommendation.Validate()
	if err != nil {
		return fmt.Errorf("invalid recommendation: %v", err)
	}

	// Check whether the associated job exists
	snap, err := r.srv.fsm.State().Snapshot()
	if err != nil {
		return fmt.Errorf("error creating snapshot: %v", err)
	}
	job, err := snap.JobByID(nil, args.Recommendation.JobNamespace, args.Recommendation.JobID)
	if err != nil {
		return fmt.Errorf("error looking up job: %v", err)
	}
	if job == nil {
		return structs.NewErrRPCCoded(400, fmt.Sprintf("job %q does not exist in namespace %q",
			args.Recommendation.JobID, args.Recommendation.JobNamespace))
	}

	// Validate the path against the job
	var group *structs.TaskGroup
	for _, g := range job.TaskGroups {
		if g.Name == path.Group {
			group = g
			break
		}
	}
	if group == nil {
		return structs.NewErrRPCCoded(400, fmt.Sprintf("group %q does not exist in job", path.Group))
	}
	var task *structs.Task
	for _, t := range group.Tasks {
		if t.Name == path.Task {
			task = t
			break
		}
	}
	if task == nil {
		return structs.NewErrRPCCoded(400, fmt.Sprintf("task %q does not exist in group %q", path.Task, path.Group))
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
	if existing != nil && existing.Path != args.Recommendation.Path {
		return structs.NewErrRPCCoded(400, "cannot update recommendation path")
	}

	// if existing == nil {
	// 	// otherwise, check whether a recommendation exists for the same path
	// 	jobRecs, err := snap.RecommendationsByJob(nil, job.Namespace, job.ID)
	// 	if err != nil {
	// 		return fmt.Errorf("recommendation lookup fail: %v", err)
	// 	}
	// 	for _, r := range jobRecs {
	// 		if r.Path == args.Recommendation.Path {
	// 			existing = r
	// 			break
	// 		}
	// 	}
	// }

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

	outRec := out.(*structs.Recommendation)

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
		if aclObj != nil && !allowNsOp(aclObj, rec.JobNamespace) {
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
