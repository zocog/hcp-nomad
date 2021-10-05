//go:build ent
// +build ent

/*
Each region's deployment within a multiregion deployment starts in a `pending`
state.  The first region is unpaused once the Job.Register gets a non-error
response back from all regions. In each pass through the deploymentwatcher's
watch method, we check the the status against nextRegion to determine if we
need to make RPCs to other regions.  A high-level summary of the state machine
is as follows:

- A `running` region returns from nextRegion with nothing to do.
- Once each region finishes `running`, the deploymentwatcher transitions it
  to `blocked`.
- If a `blocked` region is the last region, it enters `unblocking`...
  otherwise it remains `blocked` and unpauses the next region(s),
  transitioning them to `running`.
- An `unblocking` region calls unblock on all the regions, transitioning them
  to `successful`.
- A `failed` or `cancelled` deployment cancels or fails other regions,
  depending on the OnFailure config, and then exits the deploymentwatcher.
- A `successful` deployment exits the deploymentwatcher.

The diagram below shows these states. The `failed` and `cancelled` states are
omitted for clarity; any state shown here except for `successful` can
transition to the `failed` or `cancelled` state.

                                          ┌──────────┐
                                     ╭───>│successful│<───╮
┌───────┐    ┌───────┐    ┌───────┐  │    └──────────┘    │
│pending├───>│running├───>│blocked├──┤                    │
└───────┘    └───────┘    └───────┘  │    ┌──────────┐    │
                                     ╰───>│unblocking├────╯
                                          └──────────┘
*/

package deploymentwatcher

import (
	"fmt"
	"time"

	memdb "github.com/hashicorp/go-memdb"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/hashicorp/nomad/nomad/structs"
)

// DeploymentRPC are RPC methods of nomad.Deployment (nomad/deployment_endpoint.go)
type DeploymentRPC interface {
	Pause(*structs.DeploymentPauseRequest, *structs.DeploymentUpdateResponse) error
	Run(*structs.DeploymentRunRequest, *structs.DeploymentUpdateResponse) error
	Fail(*structs.DeploymentFailRequest, *structs.DeploymentUpdateResponse) error
	Unblock(*structs.DeploymentUnblockRequest, *structs.DeploymentUpdateResponse) error
	Cancel(*structs.DeploymentCancelRequest, *structs.DeploymentUpdateResponse) error
}

// JobRPC are RPC methods of nomad.Job (nomad/job_endpoint.go)
type JobRPC interface {
	Deployments(*structs.JobSpecificRequest, *structs.DeploymentListResponse) error
}

// nextRegion is called for multiregion deployments each time the delpoymentwatcher
// receives on the deployment update channel. We check the status to determin if we
// need to make RPCs to other regions.
func (w *deploymentWatcher) nextRegion(status string) error {
	job := w.j
	if !job.IsMultiregion() {
		return nil
	}
	regions := []string{}

	// make a slice of the regions that come after this one
	var found bool
	remainingRegions := []string{}

	for _, region := range job.Multiregion.Regions {
		regions = append(regions, region.Name)
		if region.Name == job.Region {
			found = true
		} else {
			if found {
				remainingRegions = append(remainingRegions, region.Name)
			}
		}
	}

	switch status {

	case structs.DeploymentStatusBlocked:
		if isLastRegion(regions, job.Region) {
			return w.checkpoint(structs.DeploymentStatusUnblocking,
				structs.DeploymentStatusDescriptionUnblocking)
		}

		// Make a collection of deployments to run.
		//
		// ideally we'd just run all the regions and act on any errors
		// we get to avoid TOCTOU races. But we need to get the deployment
		// ID from the region first, so as long as we've got a status handy
		// we might as well check it.
		deploymentIDs := make(map[string]string, len(regions))
		runningDeploysCount := 0

		for _, region := range regions {
			otherDeploy, err := w.deploymentForJobVersion(job.ID, job.Version, region)
			if err != nil {
				return err
			}
			switch otherDeploy.Status {
			case structs.DeploymentStatusPending:
				deploymentIDs[region] = otherDeploy.ID
			case structs.DeploymentStatusCancelled, structs.DeploymentStatusFailed:
				return w.checkpoint(otherDeploy.Status, otherDeploy.StatusDescription)
			case structs.DeploymentStatusRunning:
				runningDeploysCount++
			default: // running, paused, blocked, or successful: skip it
			}
		}

		// Run remaining regions, up to a maximum of the MaxParallel
		for _, region := range remainingRegions {
			otherID, ok := deploymentIDs[region]
			if !ok {
				// we reach this state if a concurrent deployment is running.
				// we've already counted it against our MaxParallel if it
				// wasn't finished
				continue
			}

			err := w.runDeployment(otherID, region)
			if err != nil {
				return err
			}

			runningDeploysCount++
			if job.Multiregion.Strategy.MaxParallel != 0 {
				if runningDeploysCount >= job.Multiregion.Strategy.MaxParallel {
					return nil
				}
			}
		}

	case structs.DeploymentStatusUnblocking:
		// note: only the final region can ever be in this state

		// Make a collection of deployments to Unblock, asserting that
		// all regions are done running.
		//
		// ideally we'd just unblock all the regions and act on any errors
		// we get to avoid TOCTOU races. But we need to get the deployment
		// ID from the region first, so as long as we've got a status handy
		// we might as well check it.
		deploymentIDs := make(map[string]string, len(regions))

		for _, region := range regions {
			if region != job.Region {
				otherDeploy, err := w.deploymentForJobVersion(job.ID, job.Version, region)
				if err != nil {
					return err
				}
				switch otherDeploy.Status {
				case structs.DeploymentStatusPaused:
					// skip it: the operator has paused the deployment
				case structs.DeploymentStatusCancelled, structs.DeploymentStatusFailed:
					return w.checkpoint(otherDeploy.Status, otherDeploy.StatusDescription)
				case structs.DeploymentStatusBlocked:
					deploymentIDs[region] = otherDeploy.ID
				case structs.DeploymentStatusRunning:
					// the other regions aren't all done, so retry
					w.deploymentUpdateCh <- struct{}{}
					return nil
				case structs.DeploymentStatusUnblocking, structs.DeploymentStatusPending:
					return w.invariantViolated(fmt.Errorf(
						"while unblocking, deployment %s for region %q had unexpected status: %s, pausing deployment",
						otherDeploy.ID, region, otherDeploy.Status))
				default: // successful: skip it
				}
			}
		}

		// Unblock all regions
		for _, region := range regions {
			if region != job.Region {
				otherID, ok := deploymentIDs[region]
				if !ok {
					// we reach this state if we're retrying from a failed checkpoint
					continue
				}
				err := w.unblockDeployment(otherID, region)
				if err != nil {
					return err
				}
			}
		}
		return w.checkpoint(structs.DeploymentStatusSuccessful,
			structs.DeploymentStatusDescriptionSuccessful)

	case structs.DeploymentStatusFailed:
		switch job.Multiregion.Strategy.OnFailure {
		case "fail_all":
			return w.abort(regions, w.failImpl)
		case "fail_local":
			return nil
		default:
			return w.abort(remainingRegions, w.failImpl)
		}
	case structs.DeploymentStatusCancelled:
		switch job.Multiregion.Strategy.OnFailure {
		case "fail_all":
			return w.abort(regions, w.cancelImpl)
		case "fail_local":
			return nil
		default:
			return w.abort(remainingRegions, w.cancelImpl)
		}
	default:
		// running, pending, or paused: no-op.
		// successful: in multiregion deployments, we only ever reach this state
		// if we are unblocked by the last region, we are the last region and have
		// successfully unblocked everyone else, or we've been manually
		// unblocked by the user. at this point this region's deployment is
		// terminal.
		return nil
	}
	return nil
}

// runDeployment runs a peer deployment in another region.
// If the deployment is somehow already successful or blocked, we'll ignore it
// and continue in the caller. But if the deployment is failed or cancelled
// we'll set our state to match and return the error. All other errors fail
// the deployment and are returned.
func (w *deploymentWatcher) runDeployment(otherID string, region string) error {
	token, err := w.tokenForJob()
	if err != nil {
		return fmt.Errorf("failed to run deploy in peer region: %v", err)
	}
	args := &structs.DeploymentRunRequest{
		DeploymentID: otherID,
		WriteRequest: structs.WriteRequest{
			Region:    region,
			Namespace: w.j.Namespace,
			AuthToken: token,
		},
	}
	err = w.Run(args, &structs.DeploymentUpdateResponse{})

	if err != nil {
		var result multierror.Error
		multierror.Append(&result, err)
		status := structs.DeploymentStatusFailed
		desc := structs.DeploymentStatusDescriptionFailedByPeer

		if err == structs.ErrDeploymentTerminalNoRun {
			otherDeploy, e := w.deploymentForJobVersion(w.j.ID, w.j.Version, region)
			if e != nil {
				multierror.Append(&result, e)
				return result.ErrorOrNil()
			}
			switch otherDeploy.Status {
			case structs.DeploymentStatusCancelled, structs.DeploymentStatusFailed:
				status = otherDeploy.Status
				desc = otherDeploy.StatusDescription
			default:
				// running, successful, or blocked: we'll typically catch the successful
				// or blocked case before getting here, but the caller will count this as
				// a running so that we don't go over our maximum.
				return nil
			}
		}
		e := w.checkpoint(status, desc)
		if e != nil {
			multierror.Append(&result, e)
		}
		return result.ErrorOrNil()
	}
	return nil
}

func (w *deploymentWatcher) tokenForJob() (string, error) {
	if w.j.NomadTokenID == "" {
		return "", nil
	}
	ws := memdb.NewWatchSet()
	aclToken, err := w.state.ACLTokenByAccessorID(ws, w.j.NomadTokenID)
	if err != nil {
		return "", err
	}
	if aclToken == nil {
		return "", fmt.Errorf("token not found: %q", w.j.NomadTokenID)
	}
	return aclToken.SecretID, nil
}

// unblockDeployment unblocks a peer deployment in another region.
// If the deployment is somehow already successful, we'll ignore it
// and continue in the caller. But if the deployment is failed, cancelled,
// or running, we'll set our state and return the error. All other errors fail
// the deployment and are returned.
func (w *deploymentWatcher) unblockDeployment(otherID, region string) error {

	token, err := w.tokenForJob()
	if err != nil {
		return fmt.Errorf("failed to unblock deploy in peer region: %v", err)
	}
	args := &structs.DeploymentUnblockRequest{
		DeploymentID: otherID,
		WriteRequest: structs.WriteRequest{
			Region:    region,
			Namespace: w.j.Namespace,
			AuthToken: token,
		},
	}
	err = w.Unblock(args, &structs.DeploymentUpdateResponse{})
	if err != nil {
		switch err {
		case structs.ErrDeploymentTerminalNoUnblock:
			var result multierror.Error
			multierror.Append(&result, err)
			otherDeploy, e := w.deploymentForJobVersion(w.j.ID, w.j.Version, region)
			if e != nil {
				multierror.Append(&result, e)
				return result.ErrorOrNil()
			}
			switch otherDeploy.Status {
			case structs.DeploymentStatusCancelled, structs.DeploymentStatusFailed:
				e := w.checkpoint(otherDeploy.Status, otherDeploy.StatusDescription)
				if e != nil {
					multierror.Append(&result, e)
				}
				return result.ErrorOrNil()
			default:
				return nil
			}
		case structs.ErrDeploymentRunningNoUnblock:
			// we've previously asserted that all regions were no longer running, so we
			// should never see this state!
			return w.invariantViolated(fmt.Errorf(
				"while unblocking, deployment %s for region %q had unexpected status: %s, pausing deployment",
				otherID, region, structs.DeploymentStatusRunning))
		default:
			return err
		}
	}
	return nil
}

// checkpoint upserts the updated status to the state store
func (w *deploymentWatcher) checkpoint(status, desc string) error {
	update := w.getDeploymentStatusUpdate(status, desc)

	// don't emit a new eval from checkpointing if no changes should
	// be made to the allocations
	if status == structs.DeploymentStatusSuccessful ||
		status == structs.DeploymentStatusBlocked ||
		status == structs.DeploymentStatusUnblocking {
		_, err := w.upsertDeploymentStatusUpdate(update, nil, nil)
		return err
	}

	eval := w.getEval()

	// we don't need to worry about the rollback here, as the caller
	// for nextRegion will always handle that
	_, err := w.upsertDeploymentStatusUpdate(update, eval, nil)
	return err
}

// invariantViolated means we're in an illegal state! our invariants
// are broken so we can't make any reasonble decisions. pause ourselves
// so that the operator can intervene
func (w *deploymentWatcher) invariantViolated(err error) error {
	var result multierror.Error
	multierror.Append(&result, err)
	err = w.checkpoint(structs.DeploymentStatusPaused,
		structs.DeploymentStatusDescriptionPaused)
	if err != nil {
		multierror.Append(&result, err)
	}
	return result.ErrorOrNil()
}

func (w *deploymentWatcher) abort(regions []string, abortImpl abortFunc) error {
	job := w.j

	var result multierror.Error
	for _, region := range regions {
		if region != job.Region {
			otherDeploy, err := w.deploymentForJobVersion(job.ID, job.Version, region)
			if err != nil {
				multierror.Append(&result, err)
				continue
			}
			switch otherDeploy.Status {
			case structs.DeploymentStatusFailed, structs.DeploymentStatusCancelled:
				// skip it: already terminal
			case structs.DeploymentStatusSuccessful:
				multierror.Append(&result, fmt.Errorf(
					"deployment %s for region %q failed but peer deployment %s for region "+
						"%q was already successful",
					w.d.ID, job.Region, otherDeploy.ID, region))
			default: // paused, blocked, running, unblocking
				err := abortImpl(otherDeploy.ID, region)
				if err == structs.ErrDeploymentTerminalNoCancel || err == structs.ErrDeploymentTerminalNoFail {
					continue
				} else {
					multierror.Append(&result, err)
				}
			}
		}
	}
	return result.ErrorOrNil()
}

type abortFunc func(string, string) error

func (w *deploymentWatcher) cancelImpl(id string, region string) error {
	token, err := w.tokenForJob()
	if err != nil {
		return fmt.Errorf("failed to cancel deploy in peer region: %v", err)
	}
	args := &structs.DeploymentCancelRequest{
		DeploymentID: id,
		WriteRequest: structs.WriteRequest{
			Region:    region,
			Namespace: w.j.Namespace,
			AuthToken: token,
		},
	}
	return w.Cancel(args, &structs.DeploymentUpdateResponse{})
}

func (w *deploymentWatcher) failImpl(id string, region string) error {
	token, err := w.tokenForJob()
	if err != nil {
		return fmt.Errorf("failed to fail deploy in peer region: %v", err)
	}
	args := &structs.DeploymentFailRequest{
		DeploymentID: id,
		WriteRequest: structs.WriteRequest{
			Region:    region,
			Namespace: w.j.Namespace,
			AuthToken: token,
		},
	}
	return w.Fail(args, &structs.DeploymentUpdateResponse{})
}

func isLastRegion(regions []string, region string) bool {
	return region == regions[len(regions)-1]
}

func (w *deploymentWatcher) deploymentForJobVersion(jobID string, version uint64, region string) (*structs.Deployment, error) {
	token, err := w.tokenForJob()
	if err != nil {
		return nil, err
	}
	args := &structs.JobSpecificRequest{
		JobID: jobID,
		QueryOptions: structs.QueryOptions{
			Namespace: w.j.Namespace,
			Region:    region,
			AuthToken: token,
		},
	}
	reply := &structs.DeploymentListResponse{}

	// Query the remote region, with retries and backoff
	retry := 0
	for {
		err := w.Deployments(args, reply)
		if err == nil {
			break
		}
		if retry > 2 {
			return nil, err
		}
		backoff := 1 << retry * time.Second
		retry++
		time.Sleep(backoff)
	}

	for _, deployment := range reply.Deployments {
		if deployment.JobVersion == version {
			return deployment, nil
		}
	}
	return nil, fmt.Errorf("no such job version")
}

// RunDeployment is used to run a pending multiregion deployment.  In
// single-region deployments, the pending state is unused.
func (w *deploymentWatcher) RunDeployment(req *structs.DeploymentRunRequest, resp *structs.DeploymentUpdateResponse) error {
	// Determine the status we should transition to and if we need to create an
	// evaluation
	var eval *structs.Evaluation
	evalID := ""
	status, desc := structs.DeploymentStatusRunning, structs.DeploymentStatusDescriptionRunning
	eval = w.getEval()
	evalID = eval.ID
	update := w.getDeploymentStatusUpdate(status, desc)

	// Commit the change
	i, err := w.upsertDeploymentStatusUpdate(update, eval, nil)
	if err != nil {
		return err
	}

	// Build the response
	if evalID != "" {
		resp.EvalID = evalID
		resp.EvalCreateIndex = i
	}
	resp.DeploymentModifyIndex = i
	resp.Index = i
	return nil
}

// UnblockDeployment is used to unblock a multiregion deployment.  In
// single-region deployments, the blocked state is unused.
func (w *deploymentWatcher) UnblockDeployment(req *structs.DeploymentUnblockRequest, resp *structs.DeploymentUpdateResponse) error {

	status, desc := structs.DeploymentStatusSuccessful, structs.DeploymentStatusDescriptionSuccessful

	// Commit the change, and in the next pass the watch() method will get the
	// update and exit the watcher
	update := w.getDeploymentStatusUpdate(status, desc)
	i, err := w.upsertDeploymentStatusUpdate(update, nil, nil)
	if err != nil {
		return err
	}
	resp.DeploymentModifyIndex = i
	resp.Index = i
	return nil
}

// CancelDeployment is used to cancel a multiregion deployment.  In
// single-region deployments, the deploymentwatcher has sole responsibility to
// cancel deployments so this RPC is never used.
func (w *deploymentWatcher) CancelDeployment(req *structs.DeploymentCancelRequest, resp *structs.DeploymentUpdateResponse) error {

	// TODO: when we wire this RPC up to the nextRegion workflow, it would be nice
	// to set the description explicitly from the DeploymentCancelRequest.
	status, desc := structs.DeploymentStatusCancelled, structs.DeploymentStatusDescriptionNewerJob

	// Commit the change, and in the next pass the watch() method will get the
	// update and exit the watcher
	update := w.getDeploymentStatusUpdate(status, desc)
	i, err := w.upsertDeploymentStatusUpdate(update, nil, nil)
	if err != nil {
		return err
	}
	resp.DeploymentModifyIndex = i
	resp.Index = i
	return nil
}
