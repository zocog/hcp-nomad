// +build ent

package nomad

import (
	"fmt"
	"strings"

	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/structs"
)

// enforceSubmitJob is used to check any Sentinel policies for the submit-job scope
func (j *Job) enforceSubmitJob(override bool, job *structs.Job) (error, error) {
	dataCB := func() map[string]interface{} {
		return map[string]interface{}{
			"job": job,
		}
	}
	return j.srv.enforceScope(override, structs.SentinelScopeSubmitJob, dataCB)
}

// interpolateMultiregionFields interpolates a job for a specific region
func (j *Job) interpolateMultiregionFields(args *structs.JobPlanRequest) error {

	// a multiregion job that's been interpolated for fan-out will never
	// have the "global" region.
	if args.Job.Region != "global" || args.Job.Multiregion == nil {
		return nil
	}

	// enterprise license enforcement - if not licensed then users can't
	// plan multiregion jobs.
	err := j.srv.EnterpriseState.FeatureCheck(license.FeatureMultiregionDeployments, true)
	if err != nil {
		return err
	}

	for _, region := range args.Job.Multiregion.Regions {
		if region.Name == j.srv.Region() {
			args.Job = regionalJob(args.Job, region)
			break
		}
	}
	return nil
}

// multiregionRegister is used to send a job across multiple regions
func (j *Job) multiregionRegister(args *structs.JobRegisterRequest, reply *structs.JobRegisterResponse) error {

	// a multiregion job that's been interpolated for fan-out will never
	// have the "global" region.
	if args.Job.Region != "global" || args.Job.Multiregion == nil {
		return nil
	}

	// enterprise license enforcement - if not licensed then users can't
	// register/update multiregion jobs.
	err := j.srv.EnterpriseState.FeatureCheck(license.FeatureMultiregionDeployments, true)
	if err != nil {
		return err
	}

	requests := []*structs.JobRegisterRequest{}
	maxVersion := args.Job.Version

	var job *structs.Job
	for _, region := range args.Job.Multiregion.Regions {
		if region.Name == j.srv.Region() {
			job = regionalJob(args.Job.Copy(), region)
		} else {
			version, jobModifyIndex, err := j.getJobVersion(args, region.Name)
			if err != nil {
				return err
			}
			if version > maxVersion {
				maxVersion = version
			}

			req := *args // copies everything except the job
			req.Job = regionalJob(args.Job.Copy(), region)
			req.Region = region.Name
			req.EnforceIndex = true
			req.JobModifyIndex = jobModifyIndex
			requests = append(requests, &req)
		}
	}

	args.Job = job
	args.Job.Version = maxVersion
	warnings := []string{}

	for _, req := range requests {
		req.Job.Version = maxVersion
		resp := &structs.JobRegisterResponse{}
		err := j.Register(req, resp)
		warnings = append(warnings, resp.Warnings)
		if err != nil {
			reply.Warnings = strings.Join(warnings, "\n")
			return fmt.Errorf("could not register job %q in region %q: %w",
				req.Job.ID, req.Region, err)
		}
	}
	reply.Warnings = strings.Join(warnings, "\n")
	return nil
}

// getJobVersion gets the job version and modify index for the job from a specific region
func (j *Job) getJobVersion(args *structs.JobRegisterRequest, region string) (uint64, uint64, error) {
	req := &structs.JobSpecificRequest{
		JobID: args.Job.ID,
		QueryOptions: structs.QueryOptions{
			Namespace: args.Namespace,
			Region:    region,
			AuthToken: args.AuthToken,
		},
	}
	resp := &structs.SingleJobResponse{}
	err := j.GetJob(req, resp)
	if err != nil {
		return 0, 0, err
	}
	if resp.Job == nil {
		return 0, 0, nil
	}
	return resp.Job.Version, resp.Job.JobModifyIndex, nil
}

// regionalJob interpolates a multiregion job for a specific region
func regionalJob(j *structs.Job, region *structs.MultiregionRegion) *structs.Job {
	j.Region = region.Name
	j.Datacenters = region.Datacenters

	for _, tg := range j.TaskGroups {
		if tg.Count == 0 {
			tg.Count = region.Count
		}
	}

	// Override the job meta with the region meta. The job meta doesn't
	// get merged with the group/task meta until it lands on the client.
	for k, v := range region.Meta {
		if j.Meta == nil {
			j.Meta = map[string]string{}
		}
		j.Meta[k] = v
	}
	return j
}
