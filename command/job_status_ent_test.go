//go:build ent
// +build ent

package command

import (
	"fmt"
	"testing"

	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/command/agent"
	"github.com/hashicorp/nomad/testutil"
	"github.com/mitchellh/cli"
	"github.com/shoenig/test/must"
)

// TestJobStatusCommand_Multiregion tests multiregion deployment output
func TestJobStatusCommand_Multiregion(t *testing.T) {
	ci.Parallel(t)

	cbe := func(config *agent.Config) {
		config.Region = "east"
		config.Datacenter = "east-1"
	}
	cbw := func(config *agent.Config) {
		config.Region = "west"
		config.Datacenter = "west-1"
	}

	srv, clientEast, url := testServer(t, true, cbe)
	defer srv.Shutdown()

	srv2, clientWest, _ := testServer(t, true, cbw)
	defer srv2.Shutdown()

	// Join with srv1
	addr := fmt.Sprintf("127.0.0.1:%d",
		srv.Agent.Server().GetConfig().SerfConfig.MemberlistConfig.BindPort)

	if _, err := srv2.Agent.Server().Join([]string{addr}); err != nil {
		t.Fatalf("Join err: %v", err)
	}

	// wait for client node
	testutil.WaitForResult(func() (bool, error) {
		nodes, _, err := clientEast.Nodes().List(nil)
		if err != nil {
			return false, err
		}
		if len(nodes) == 0 {
			return false, fmt.Errorf("missing node")
		}
		if _, ok := nodes[0].Drivers["mock_driver"]; !ok {
			return false, fmt.Errorf("mock_driver not ready")
		}
		return true, nil
	}, func(err error) {
		t.Fatalf("err: %s", err)
	})

	ui := new(cli.MockUi)
	cmd := &JobStatusCommand{Meta: Meta{Ui: ui, flagAddress: url}}

	// Register multiregion job in east
	jobEast := testMultiRegionJob("job1_sfxx", "east", "east-1")
	resp, _, err := clientEast.Jobs().Register(jobEast, nil)
	must.NoError(t, err)
	code := waitForSuccess(ui, clientEast, fullId, t, resp.EvalID)
	if code != 1 {
		t.Fatalf("expected monitor to show blocked deployment: %d", code)
	}

	jobs, _, err := clientEast.Jobs().List(&api.QueryOptions{})
	must.NoError(t, err)
	must.Len(t, 1, jobs)

	deploys, _, err := clientEast.Jobs().Deployments(jobs[0].ID, true, &api.QueryOptions{})
	must.NoError(t, err)
	must.Len(t, 1, deploys)

	// Grab both deployments to verify output
	eastDeploys, _, err := clientEast.Jobs().Deployments(jobs[0].ID, true, &api.QueryOptions{Region: "east"})
	must.NoError(t, err)
	must.Len(t, 1, eastDeploys)

	westDeploys, _, err := clientWest.Jobs().Deployments(jobs[0].ID, true, &api.QueryOptions{Region: "west"})
	must.NoError(t, err)

	// Run command for specific deploy
	if code := cmd.Run([]string{"-address=" + url, jobs[0].ID}); code != 0 {
		t.Fatalf("expected exit 0, got: %d", code)
	}

	// Verify Multi-region Deployment info populated
	out := ui.OutputWriter.String()
	must.StrContains(t, out, "Multiregion Deployment")
	must.StrContains(t, out, "Region")
	must.StrContains(t, out, "ID")
	must.StrContains(t, out, "Status")
	must.StrContains(t, out, "east")
	must.StrContains(t, out, eastDeploys[0].ID[0:7])
	must.StrContains(t, out, "west")
	must.StrContains(t, out, westDeploys[0].ID[0:7])

	// this will always be pending because we're not really doing a multiregion
	// register here in OSS
	must.StrContains(t, out, "pending")

	must.StrNotContains(t, out, "<none>")

}
