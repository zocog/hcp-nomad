//go:build ent
// +build ent

package deploymentwatcher

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/stretchr/testify/require"
)

// TestDeploymentwatch_NextRegion tests scenarios for multi-region deployments.
func TestDeploymentwatch_NextRegion(t *testing.T) {
	t.Parallel()

	// Each test case starts with 4 regions in a `pending` state, and can
	// override the behavior for each region by configuring a TestRegion.
	// Then we Run the 1st region, run the whole deployment, and assert
	// the resulting states. Because we run concurrently, we can't assert the
	// exact number of RPC calls (unless MaxParallel is set), just the maximum
	// value.
	cases := []testCase{

		{
			// happy scenario
			Name:           "simple success",
			Strategy:       &structs.MultiregionStrategy{OnFailure: "fail_all"},
			MaxResumes:     map[string]int{"north": 1, "south": 3, "east": 3, "west": 3},
			MaxUnblocks:    map[string]int{"north": 1, "south": 1, "east": 1},
			ExpectedStates: allSuccessful,
		},

		{
			// happy scenario, with a max parallel of 1 set
			Name:           "successful with max parallel",
			Strategy:       &structs.MultiregionStrategy{MaxParallel: 1},
			MaxResumes:     map[string]int{"north": 1, "south": 1, "east": 1, "west": 1},
			MaxUnblocks:    map[string]int{"north": 1, "south": 1, "east": 1},
			ExpectedStates: allSuccessful,
		},

		{
			// the last region fails, so all regions should be failed.
			Name:     "last region failed",
			Strategy: &structs.MultiregionStrategy{OnFailure: "fail_all"},
			TestRegions: []*MockRPCServer{
				newMockRPC("west").
					nextRun(structs.DeploymentStatusFailed, nil),
			},
			MaxResumes:     map[string]int{"north": 1, "south": 1, "east": 2, "west": 2},
			MaxFails:       map[string]int{"north": 3, "south": 3, "east": 3},
			ExpectedStates: allFailed,
		},

		{
			// the last region fails, so all regions should be failed.
			Name:     "last region failed with max parallel",
			Strategy: &structs.MultiregionStrategy{MaxParallel: 1, OnFailure: "fail_all"},
			TestRegions: []*MockRPCServer{
				newMockRPC("west").
					nextRun(structs.DeploymentStatusFailed, nil),
			},
			MaxResumes:     map[string]int{"north": 1, "south": 1, "east": 1, "west": 1},
			MaxFails:       map[string]int{"north": 1, "south": 3, "east": 3},
			ExpectedStates: allFailed,
		},

		{
			// a deploy midway thru fails, so all regions should be failed.
			Name:     "random region failed",
			Strategy: &structs.MultiregionStrategy{MaxParallel: 1, OnFailure: "fail_all"},
			TestRegions: []*MockRPCServer{
				newMockRPC("east").
					nextRun(structs.DeploymentStatusFailed, structs.ErrDeploymentTerminalNoResume),
			},
			MaxResumes:     map[string]int{"north": 1, "south": 1, "east": 1, "west": 1},
			MaxFails:       map[string]int{"north": 1, "south": 3, "west": 3},
			ExpectedStates: allFailed,
		},

		{
			// the last region succeeds, but when we unblock a peer deploy,
			// the peer deployment has been cancelled by a more recent deploy.
			// so we should cancel this and all other deployments.
			Name:     "unblocking deploy cancelled by a more recent deploy",
			Strategy: &structs.MultiregionStrategy{MaxParallel: 1, OnFailure: "fail_all"},
			TestRegions: []*MockRPCServer{
				newMockRPC("north").
					nextRun(structs.DeploymentStatusBlocked, nil).
					nextUnblock(
						structs.DeploymentStatusCancelled, structs.ErrDeploymentTerminalNoUnblock).
					nextFail(structs.DeploymentStatusFailed, nil).
					nextCancel(structs.DeploymentStatusCancelled, nil),
			},
			MaxResumes:     map[string]int{"north": 1, "south": 1, "east": 1, "west": 1},
			MaxUnblocks:    map[string]int{"north": 1},
			MaxCancels:     map[string]int{"north": 3, "south": 3, "east": 3, "west": 3},
			ExpectedStates: allCancelled,
		},

		{
			// the last region succeeds, but when we unblock a peer deploy,
			// the peer deployment has been cancelled by a more recent deploy.
			// so we should cancel this and all other deployments.
			Name:     "unblocking deploy with failed peer",
			Strategy: &structs.MultiregionStrategy{MaxParallel: 1, OnFailure: "fail_all"},
			TestRegions: []*MockRPCServer{
				newMockRPC("north").
					nextRun(structs.DeploymentStatusBlocked, nil).
					nextUnblock(structs.DeploymentStatusFailed, structs.ErrDeploymentTerminalNoUnblock).
					nextFail(structs.DeploymentStatusFailed, nil),
			},
			MaxResumes:     map[string]int{"north": 1, "south": 1, "east": 1, "west": 1},
			MaxUnblocks:    map[string]int{"north": 1},
			MaxCancels:     map[string]int{"north": 3, "south": 3, "east": 3, "west": 3},
			ExpectedStates: allFailed,
		},

		{
			// the last region succeeds, but when we unblock a peer deploy,
			// the peer deployment is in a failed state but throws an unexpected
			// error. in this case we lose the TOCTOU query and get an error back
			// from the unblock RPC. we should cancel this and all other
			// deployments.
			Name:     "unblocking deploy gets unexpected error",
			Strategy: &structs.MultiregionStrategy{MaxParallel: 1, OnFailure: "fail_all"},
			TestRegions: []*MockRPCServer{
				newMockRPC("north").
					nextRun(structs.DeploymentStatusBlocked, nil).
					nextUnblock(structs.DeploymentStatusFailed,
						errors.New("some weird error")).
					nextFail(structs.DeploymentStatusFailed, nil),
			},
			MaxResumes:     map[string]int{"north": 1, "south": 1, "east": 1, "west": 1},
			MaxFails:       map[string]int{"north": 1, "south": 2, "east": 3, "west": 3},
			MaxUnblocks:    map[string]int{"north": 1, "south": 1, "east": 1, "west": 1},
			ExpectedStates: allFailed,
		},

		{
			// we're in the process of unblocking remaining peer deploys but
			// a peer deployment has been cancelled by a more recent deploy.
			// we should cancel all remaining deployments but some will
			// be left successful. this leaves us in an inconsistent state
			// that the operator will need to resolve.
			Name:     "successful deploys do not get cancelled",
			Strategy: &structs.MultiregionStrategy{MaxParallel: 1, OnFailure: "fail_all"},
			TestRegions: []*MockRPCServer{
				newMockRPC("south").
					nextRun(structs.DeploymentStatusBlocked, nil).
					nextUnblock(structs.DeploymentStatusCancelled,
						errors.New("deployment was cancelled")),
			},
			MaxResumes:  map[string]int{"north": 1, "south": 1, "east": 1, "west": 1},
			MaxUnblocks: map[string]int{"north": 1, "south": 1},
			MaxCancels:  map[string]int{"east": 2, "west": 2},
			ExpectedStates: map[string]string{
				"north": "successful", "south": "cancelled",
				"east": "cancelled", "west": "cancelled"},
			// ExpectedErrors: map[string]int{"south": 1, "east": 1, "west": 1},
		},

		{
			// the unblocking region has a leadership transition after it's
			// unblocked all peers but before it checkpoints as successful.
			// the retry will result in a successful deployment
			Name:     "unblocking region has leader election happy case",
			Strategy: &structs.MultiregionStrategy{MaxParallel: 1, OnFailure: "fail_all"},
			TestRegions: []*MockRPCServer{
				newMockRPC("west").
					nextRun(structs.DeploymentStatusBlocked, nil).
					nextUnblock(structs.DeploymentStatusCancelled, nil).
					nextFail(structs.DeploymentStatusFailed, nil).
					nextCancel(structs.DeploymentStatusCancelled, nil).
					nextCheckpoint(structs.DeploymentStatusSuccessful,
						errors.New("misc raft error")),
			},
			MaxUnblocks:    map[string]int{"north": 1},
			ExpectedStates: allSuccessful,
		},

		{
			// the last region succeeds, but when we unblock a peer deploy,
			// we find the peer is in the running state which should be
			// impossible. fail into a safe condition.
			Name:     "unblocking deploy broken invariant",
			Strategy: &structs.MultiregionStrategy{MaxParallel: 1, OnFailure: "fail_all"},
			TestRegions: []*MockRPCServer{
				newMockRPC("north").
					nextRun(structs.DeploymentStatusBlocked, nil).
					nextUnblock(structs.DeploymentStatusRunning, structs.ErrDeploymentRunningNoUnblock),
			},
			MaxResumes:  map[string]int{"north": 1, "south": 1, "east": 1, "west": 1},
			MaxFails:    map[string]int{"north": 1, "south": 2, "east": 3, "west": 3},
			MaxUnblocks: map[string]int{"north": 1, "south": 1, "east": 1, "west": 1},
			ExpectedStates: map[string]string{
				"north": "running", "south": "blocked",
				"east": "blocked", "west": "paused"},
			ExpectedErrors: map[string]int{"west": 1},
		},

		{
			// the last region succeeds, but when we unblock a peer deploy,
			// we find the peer is in the successful state which should be
			// impossible but it's safe to continue.
			Name:     "unblocking successful deploy",
			Strategy: &structs.MultiregionStrategy{MaxParallel: 1, OnFailure: "fail_all"},
			TestRegions: []*MockRPCServer{
				newMockRPC("north").
					nextRun(structs.DeploymentStatusBlocked, nil).
					nextUnblock(structs.DeploymentStatusSuccessful, structs.ErrDeploymentTerminalNoUnblock),
			},
			MaxResumes:     map[string]int{"north": 1, "south": 1, "east": 1, "west": 1},
			MaxFails:       map[string]int{"north": 1, "south": 2, "east": 3, "west": 3},
			MaxUnblocks:    map[string]int{"north": 1, "south": 1, "east": 1, "west": 1},
			ExpectedStates: allSuccessful,
			ExpectedErrors: map[string]int{"west": 0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			require := require.New(t)
			var wg sync.WaitGroup
			watchers, cancel := tc.setup(t, tc.TestRegions)
			defer cancel()

			for _, w := range watchers {
				wg.Add(1)
				srv := w.DeploymentRPC.(*MockRPCServer)
				go srv.watch(&wg)
			}

			first := watchers["north"]
			args := &structs.DeploymentRunRequest{DeploymentID: first.d.ID}
			args.Region = "north"
			first.Run(args, &structs.DeploymentUpdateResponse{})
			wg.Wait()

			for region, watcher := range watchers {
				srv := watcher.DeploymentRPC.(*MockRPCServer)

				require.Equal(tc.ExpectedStates[region], srv.w.getStatus(),
					"expected region %q to have state %q",
					region, tc.ExpectedStates[region])

				// because the test runs the deployments concurrently, we can't assert
				// the exact number of RPCs except in the case of MaxParallel = 1
				require.LessOrEqual(int(srv.countResumeCalls), tc.MaxResumes[region],
					"expected region %q to have at most %d resume calls",
					region, tc.MaxResumes[region])
				require.LessOrEqual(int(srv.countFailCalls), tc.MaxFails[region],
					"expected region %q to have at most %d fail calls",
					region, tc.MaxFails[region])
				require.LessOrEqual(int(srv.countUnblockCalls), tc.MaxUnblocks[region],
					"expected region %q to have at most %d unblock calls",
					region, tc.MaxUnblocks[region])
				require.LessOrEqual(int(srv.countCancelCalls), tc.MaxCancels[region],
					"expected region %q to have at most %d cancel calls",
					region, tc.MaxCancels[region])

				// we only check for specific error counts because concurrent
				// deployments might cause errors to land on different regions.
				// the errors have UUIDs, so we're not going to bother comparing
				// them exactly.
				expectedErrs, ok := tc.ExpectedErrors[region]
				if ok {
					require.Equal(expectedErrs, len(srv.nextRegionErrors),
						"expected region %q to have %d errors, got: %v",
						region, expectedErrs, srv.nextRegionErrors)
				}

			}
		})
	}
}

var allSuccessful = map[string]string{
	"north": "successful", "south": "successful",
	"east": "successful", "west": "successful"}
var allFailed = map[string]string{
	"north": "failed", "south": "failed",
	"east": "failed", "west": "failed"}
var allCancelled = map[string]string{
	"north": "cancelled", "south": "cancelled",
	"east": "cancelled", "west": "cancelled"}

type testCase struct {
	Name        string
	Strategy    *structs.MultiregionStrategy
	TestRegions []*MockRPCServer

	MaxResumes     map[string]int
	MaxFails       map[string]int
	MaxUnblocks    map[string]int
	MaxCancels     map[string]int
	ExpectedStates map[string]string
	ExpectedErrors map[string]int
}

func (tc testCase) setup(t *testing.T, testRegions []*MockRPCServer) (map[string]*deploymentWatcher, context.CancelFunc) {

	job := mock.Job()
	job.Multiregion = &structs.Multiregion{
		Strategy: tc.Strategy,
		Regions: []*structs.MultiregionRegion{
			{Name: "north"},
			{Name: "south"},
			{Name: "east"},
			{Name: "west"},
		},
	}
	if job.Multiregion.Strategy == nil {
		job.Multiregion.Strategy = &structs.MultiregionStrategy{}
	}

	regionRpcs := map[string]*MockRPCServer{}
	regionNames := []string{"north", "south", "east", "west"}

	defaultMock := func(regionName string) *MockRPCServer {
		return newMockRPC(regionName).
			nextRun(structs.DeploymentStatusBlocked, nil).
			nextPause(structs.DeploymentStatusPaused, nil).
			nextResume(structs.DeploymentStatusBlocked, nil).
			nextUnblock(structs.DeploymentStatusSuccessful, nil).
			nextCancel(structs.DeploymentStatusCancelled, nil).
			nextFail(structs.DeploymentStatusFailed, nil)
	}

	for _, regionName := range regionNames {
		regionRpcs[regionName] = defaultMock(regionName)
		for _, regionRpc := range testRegions {
			if regionRpc.region == regionName {
				regionRpcs[regionName] = regionRpc
				break
			}
		}
	}

	watchers := map[string]*deploymentWatcher{}
	pctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	for regionName, regionRpc := range regionRpcs {
		d := mock.Deployment()
		d.JobID = job.ID
		d.JobVersion = job.Version
		d.Status = structs.DeploymentStatusPending // all tests start with this status

		j := job.Copy()
		j.Region = regionName

		ctx, _ := context.WithCancel(pctx)
		w := &deploymentWatcher{
			deploymentID:       d.ID,
			deploymentUpdateCh: make(chan struct{}, 2),
			d:                  d,
			j:                  j,
			deploymentTriggers: regionRpc,
			DeploymentRPC:      regionRpc,
			JobRPC:             regionRpc,
			ctx:                ctx,
		}
		regionRpc.w = w
		regionRpc.otherRegions = regionRpcs
		regionRpc.t = t
		watchers[regionName] = w
	}

	return watchers, cancel

}

type testResponse struct {
	status string
	desc   string
	err    error
}

type MockRPCServer struct {
	t      *testing.T
	w      *deploymentWatcher
	region string

	nextRunResponses []testResponse
	countRunCalls    int32

	nextPauseResponses []testResponse
	countPauseCalls    int32

	nextResumeResponses []testResponse
	countResumeCalls    int32

	nextUnblockResponses []testResponse
	countUnblockCalls    int32

	nextCancelResponses []testResponse
	countCancelCalls    int32

	nextFailResponses []testResponse
	countFailCalls    int32

	nextCheckpoints map[string][]error

	otherRegions     map[string]*MockRPCServer
	nextRegionErrors []error
}

func newMockRPC(region string) *MockRPCServer {
	return &MockRPCServer{
		region:               region,
		nextPauseResponses:   []testResponse{},
		nextResumeResponses:  []testResponse{},
		nextUnblockResponses: []testResponse{},
		nextCancelResponses:  []testResponse{},
		nextFailResponses:    []testResponse{},
		nextCheckpoints:      map[string][]error{},
		otherRegions:         map[string]*MockRPCServer{},
		nextRegionErrors:     []error{},
	}
}

// watch stands in for the deploymentwatcher's watch method, which will make calls
// to nextRegion whenever a deployment has been updated.
func (srv *MockRPCServer) watch(wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-srv.w.ctx.Done():
			return
		case <-srv.w.deploymentUpdateCh:
			err := srv.w.nextRegion(srv.w.getStatus())
			if err != nil {
				srv.nextRegionErrors = append(srv.nextRegionErrors, err)
			}
			// run one last time before exiting
			if !srv.w.d.Active() {
				err := srv.w.nextRegion(srv.w.getStatus())
				if err != nil {
					srv.nextRegionErrors = append(srv.nextRegionErrors, err)
				}
				return
			}
			// for testing only; the real deploymentwatcher will continue to
			// watch for changes but we want to exit the test if we re-enter
			// this state
			if srv.w.getStatus() == structs.DeploymentStatusPaused {
				return
			}
		}
	}
}

// MockRPCServer implements deploymentTriggers but only needs one method; the rest
// have empty implementations to satisfy the interface

func (srv *MockRPCServer) createUpdate(_ map[string]*structs.DesiredTransition, _ *structs.Evaluation) (uint64, error) {
	return 0, nil
}

func (srv *MockRPCServer) upsertJob(_ *structs.Job) (uint64, error) { return 0, nil }

func (srv *MockRPCServer) upsertDeploymentPromotion(_ *structs.ApplyDeploymentPromoteRequest) (uint64, error) {
	return 0, nil
}

func (srv *MockRPCServer) upsertDeploymentAllocHealth(_ *structs.ApplyDeploymentAllocHealthRequest) (uint64, error) {
	return 0, nil
}

func (srv *MockRPCServer) upsertDeploymentStatusUpdate(u *structs.DeploymentStatusUpdate, eval *structs.Evaluation, job *structs.Job) (uint64, error) {
	status := u.Status
	srv.w.l.Lock()
	defer func() {
		srv.w.l.Unlock()
		srv.w.deploymentUpdateCh <- struct{}{} // trigger the nextRegion
	}()

	errs, ok := srv.nextCheckpoints[status]
	if !ok || len(errs) == 0 {
		srv.w.d.Status = status
		srv.w.d.StatusDescription = u.StatusDescription
		return 0, nil
	}
	next := errs[0]
	srv.nextCheckpoints[status] = errs[1:]
	return 0, next
}

func (srv *MockRPCServer) isActive() bool {
	srv.w.l.RLock()
	defer srv.w.l.RUnlock()
	return srv.w.d.Active()
}

// Run implements the DeploymentRPC interface to run a pending deployment.
func (srv *MockRPCServer) Run(args *structs.DeploymentRunRequest, reply *structs.DeploymentUpdateResponse) error {
	if args.Region != srv.region { // forward to another region
		return srv.otherRegions[args.Region].Run(args, reply)
	}
	return srv.rpcImpl(srv.region, srv.nextRunResponses, srv.countRunCalls,
		structs.ErrDeploymentTerminalNoRun)
}

// Pause implements the DeploymentRPC interface to pause/resume a deployment.
func (srv *MockRPCServer) Pause(args *structs.DeploymentPauseRequest, reply *structs.DeploymentUpdateResponse) error {
	if args.Region != srv.region { // forward to another region
		return srv.otherRegions[args.Region].Pause(args, reply)
	}
	if args.Pause {
		return srv.rpcImpl(srv.region, srv.nextPauseResponses, srv.countPauseCalls,
			structs.ErrDeploymentTerminalNoPause)
	}
	return srv.rpcImpl(srv.region, srv.nextResumeResponses, srv.countResumeCalls,
		structs.ErrDeploymentTerminalNoResume)
}

// Fail implements the DeploymentRPC interface to fail a deployment.
func (srv *MockRPCServer) Fail(args *structs.DeploymentFailRequest, reply *structs.DeploymentUpdateResponse) error {
	if args.Region != srv.region { // forward to another region
		return srv.otherRegions[args.Region].Fail(args, reply)
	}
	return srv.rpcImpl(srv.region, srv.nextFailResponses, srv.countFailCalls,
		structs.ErrDeploymentTerminalNoFail)
}

// Unblock implements the DeploymentRPC interface to unblock a deployment.
func (srv *MockRPCServer) Unblock(args *structs.DeploymentUnblockRequest, reply *structs.DeploymentUpdateResponse) error {
	if args.Region != srv.region { // forward to another region
		return srv.otherRegions[args.Region].Unblock(args, reply)
	}
	return srv.rpcImpl(srv.region, srv.nextUnblockResponses, srv.countUnblockCalls,
		structs.ErrDeploymentTerminalNoUnblock)
}

// Cancel implements the DeploymentRPC interface to cancel a deployment.
func (srv *MockRPCServer) Cancel(args *structs.DeploymentCancelRequest, reply *structs.DeploymentUpdateResponse) error {
	if args.Region != srv.region { // forward to another region
		return srv.otherRegions[args.Region].Cancel(args, reply)
	}
	return srv.rpcImpl(srv.region, srv.nextCancelResponses, srv.countCancelCalls,
		structs.ErrDeploymentTerminalNoCancel)
}

func (srv *MockRPCServer) rpcImpl(region string, responses []testResponse, idx int32, err error) error {
	var resp testResponse
	defer atomic.AddInt32(&idx, 1)
	if len(responses) == 0 {
		srv.t.Fatalf("test region %q is missing responses", srv.region)
	}
	if int(idx) >= len(responses) {
		resp = responses[len(responses)-1] // just repeat the last response
	} else {
		resp = responses[idx]
	}
	if !srv.isActive() {
		return err
	}
	srv.w.checkpoint(resp.status, "whatever") // we don't test for description
	return resp.err
}

// Deployments implements the JobRPC interface to fetch the deployments for
// a specific version of a job.
func (srv *MockRPCServer) Deployments(args *structs.JobSpecificRequest, reply *structs.DeploymentListResponse) error {
	if args.Region != srv.region {
		return srv.otherRegions[args.Region].Deployments(args, reply)
	}
	reply.Deployments = []*structs.Deployment{srv.w.d}
	return nil
}

func (srv *MockRPCServer) nextResponse(responses []testResponse, idx int32) testResponse {
	defer atomic.AddInt32(&idx, 1)
	if len(responses) == 0 {
		srv.t.Fatalf("test region %q is missing responses", srv.region)
	}
	if int(idx) >= len(responses) {
		return responses[len(responses)-1] // just repeat the last response
	}
	return responses[idx]
}

func (srv *MockRPCServer) nextRun(status string, err error) *MockRPCServer {
	srv.nextRunResponses = append(srv.nextRunResponses, testResponse{status, status, err})
	return srv
}

func (srv *MockRPCServer) nextPause(status string, err error) *MockRPCServer {
	srv.nextPauseResponses = append(srv.nextPauseResponses, testResponse{status, status, err})
	return srv
}

func (srv *MockRPCServer) nextResume(status string, err error) *MockRPCServer {
	srv.nextResumeResponses = append(srv.nextResumeResponses, testResponse{status, status, err})
	return srv
}

func (srv *MockRPCServer) nextUnblock(status string, err error) *MockRPCServer {
	srv.nextUnblockResponses = append(srv.nextUnblockResponses, testResponse{status, status, err})
	return srv
}

func (srv *MockRPCServer) nextFail(status string, err error) *MockRPCServer {
	srv.nextFailResponses = append(srv.nextFailResponses, testResponse{status, status, err})
	return srv
}

func (srv *MockRPCServer) nextCancel(status string, err error) *MockRPCServer {
	srv.nextCancelResponses = append(srv.nextCancelResponses, testResponse{status, status, err})
	return srv
}

func (srv *MockRPCServer) nextCheckpoint(status string, err error) *MockRPCServer {
	errs, ok := srv.nextCheckpoints[status]
	if ok {
		srv.nextCheckpoints[status] = append(errs, err)
	} else {
		srv.nextCheckpoints[status] = []error{err}
	}
	return srv
}
