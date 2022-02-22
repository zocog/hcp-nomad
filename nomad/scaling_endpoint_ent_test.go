//go:build ent
// +build ent

package nomad

import (
	"testing"

	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/nomad/acl"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
)

func TestScalingEndpoint_List_MultiNamespace(t *testing.T) {
	t.Parallel()
	s1, cleanupS1 := TestServer(t, nil)
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)
	state := s1.State()

	// two namespaces
	ns1 := mock.Namespace()
	ns2 := mock.Namespace()
	require.NoError(t, state.UpsertNamespaces(900, []*structs.Namespace{ns1, ns2}))

	// job1a
	job1a := mock.Job()
	job1a.Namespace = ns1.Name
	job1a.TaskGroups = append(job1a.TaskGroups, job1a.TaskGroups[0].Copy())
	job1a.TaskGroups[1].Name = "second group"
	job1a.TaskGroups[1].Tasks = append(job1a.TaskGroups[1].Tasks, job1a.TaskGroups[1].Tasks[0].Copy())
	job1a.TaskGroups[1].Tasks[1].Name = "second task"
	pol1a0H := mock.ScalingPolicy()
	pol1a0H.ModifyIndex = 901
	pol1a0H.CreateIndex = 901
	pol1a0H.ID = "aa" + pol1a0H.ID[2:]
	pol1a0H.Type = structs.ScalingPolicyTypeHorizontal
	pol1a0H.TargetTaskGroup(job1a, job1a.TaskGroups[0])
	job1a.TaskGroups[0].Scaling = pol1a0H
	pol1a1H := mock.ScalingPolicy()
	pol1a1H.CreateIndex = 901
	pol1a1H.ModifyIndex = 901
	pol1a1H.ID = "bb" + pol1a1H.ID[2:]
	pol1a1H.Type = structs.ScalingPolicyTypeHorizontal
	pol1a1H.TargetTaskGroup(job1a, job1a.TaskGroups[1])
	job1a.TaskGroups[1].Scaling = pol1a1H
	pol1a1C := mock.ScalingPolicy()
	pol1a1C.CreateIndex = 901
	pol1a1C.ModifyIndex = 901
	pol1a1C.ID = "bb" + pol1a1C.ID[2:]
	pol1a1C.Type = structs.ScalingPolicyTypeVerticalCPU
	pol1a1C.TargetTask(job1a, job1a.TaskGroups[1], job1a.TaskGroups[1].Tasks[0])
	job1a.TaskGroups[1].Tasks[0].ScalingPolicies = append(job1a.TaskGroups[1].Tasks[0].ScalingPolicies, pol1a1C)
	pol1a1M := mock.ScalingPolicy()
	pol1a1M.CreateIndex = 901
	pol1a1M.ModifyIndex = 901
	pol1a1M.ID = "cc" + pol1a1M.ID[2:]
	pol1a1M.Type = structs.ScalingPolicyTypeVerticalMem
	pol1a1M.TargetTask(job1a, job1a.TaskGroups[1], job1a.TaskGroups[1].Tasks[0])
	job1a.TaskGroups[1].Tasks[0].ScalingPolicies = append(job1a.TaskGroups[1].Tasks[0].ScalingPolicies, pol1a1M)
	require.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 901, job1a))

	// job1b
	job1b := mock.Job()
	job1b.Namespace = ns1.Name
	pol1bH := mock.ScalingPolicy()
	pol1bH.CreateIndex = 902
	pol1bH.ModifyIndex = 902
	pol1bH.ID = "dd" + pol1bH.ID[2:]
	pol1bH.TargetTaskGroup(job1b, job1b.TaskGroups[0])
	job1b.TaskGroups[0].Scaling = pol1bH
	require.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 902, job1b))

	// job2
	job2 := mock.Job()
	job2.Namespace = ns2.Name
	pol2H := mock.ScalingPolicy()
	pol2H.CreateIndex = 903
	pol2H.ModifyIndex = 903
	pol2H.ID = "aa" + pol2H.ID[2:]
	pol2H.TargetTaskGroup(job2, job2.TaskGroups[0])
	job2.TaskGroups[0].Scaling = pol2H
	pol2C := mock.ScalingPolicy()
	pol2C.CreateIndex = 903
	pol2C.ModifyIndex = 903
	pol2C.ID = "bb" + pol2H.ID[2:]
	pol2C.Type = structs.ScalingPolicyTypeVerticalCPU
	pol2C.TargetTask(job2, job2.TaskGroups[0], job2.TaskGroups[0].Tasks[0])
	job2.TaskGroups[0].Tasks[0].ScalingPolicies = []*structs.ScalingPolicy{pol2C}
	require.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 903, job2))

	cases := []struct {
		Label     string
		Namespace string
		Type      string
		Prefix    string
		Job       string
		Pols      []*structs.ScalingPolicy
	}{
		{
			Label:     "all namespaces",
			Namespace: "*",
			Pols:      []*structs.ScalingPolicy{pol1a0H, pol1a1C, pol1a1H, pol1a1M, pol1bH, pol2H, pol2C},
		},
		{
			Label:     "all namespaces with ID prefix",
			Namespace: "*",
			Prefix:    pol1a0H.ID[0:2],
			Pols:      []*structs.ScalingPolicy{pol1a0H, pol2H},
		},
		{
			Label:     "all namespaces with non-matching ID prefix",
			Namespace: "*",
			Prefix:    "00",
			Pols:      []*structs.ScalingPolicy{},
		},
		{
			Label:     "ns1",
			Namespace: ns1.Name,
			Pols:      []*structs.ScalingPolicy{pol1a0H, pol1a1C, pol1a1H, pol1a1M, pol1bH},
		},
		{
			Label:     "ns1 horizontal",
			Namespace: ns1.Name,
			Type:      structs.ScalingPolicyTypeHorizontal,
			Pols:      []*structs.ScalingPolicy{pol1a0H, pol1a1H, pol1bH},
		},
		{
			Label:     "ns1 vertical",
			Namespace: ns1.Name,
			Type:      "vertical",
			Pols:      []*structs.ScalingPolicy{pol1a1C, pol1a1M},
		},
		{
			Label:     "ns1 verical cpu",
			Namespace: ns1.Name,
			Type:      structs.ScalingPolicyTypeVerticalCPU,
			Pols:      []*structs.ScalingPolicy{pol1a1C},
		},
		{
			Label:     "ns1 verical mem",
			Namespace: ns1.Name,
			Type:      structs.ScalingPolicyTypeVerticalMem,
			Pols:      []*structs.ScalingPolicy{pol1a1M},
		},
		{
			Label:     "ns1 with ID prefix",
			Namespace: ns1.Name,
			Prefix:    pol1a0H.ID[0:2],
			Pols:      []*structs.ScalingPolicy{pol1a0H},
		},
		{
			Label:     "ns2",
			Namespace: ns2.Name,
			Pols:      []*structs.ScalingPolicy{pol2H, pol2C},
		},
		{
			Label:     "bad namespace",
			Namespace: uuid.Generate(),
			Pols:      []*structs.ScalingPolicy{},
		},
		{
			Label:     "all namespaces horizontal",
			Namespace: structs.AllNamespacesSentinel,
			Type:      structs.ScalingPolicyTypeHorizontal,
			Pols:      []*structs.ScalingPolicy{pol1a0H, pol1a1H, pol1bH, pol2H},
		},
		{
			Label:     "all namespaces all vertical",
			Namespace: structs.AllNamespacesSentinel,
			Type:      "vertical",
			Pols:      []*structs.ScalingPolicy{pol2C, pol1a1C, pol1a1M},
		},
		{
			Label:     "job level with multiple",
			Namespace: ns1.Name,
			Job:       job1a.ID,
			Pols:      []*structs.ScalingPolicy{pol1a0H, pol1a1H, pol1a1M, pol1a1C},
		},
		{
			Label:     "job level with single",
			Namespace: ns2.Name,
			Job:       job2.ID,
			Pols:      []*structs.ScalingPolicy{pol2H, pol2C},
		},
		{
			Label:     "job level for missing job",
			Namespace: ns1.Name,
			Job:       "missing job",
			Pols:      []*structs.ScalingPolicy{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			// wrong namespace still works in the absence of ACLs
			var resp structs.ScalingPolicyListResponse
			err := msgpackrpc.CallWithCodec(
				codec,
				"Scaling.ListPolicies",
				&structs.ScalingPolicyListRequest{
					Job:  tc.Job,
					Type: tc.Type,
					QueryOptions: structs.QueryOptions{
						Region:    "global",
						Namespace: tc.Namespace,
						Prefix:    tc.Prefix,
					},
				}, &resp)
			require.NoError(t, err)
			exp := make([]*structs.ScalingPolicyListStub, len(tc.Pols))
			for i, p := range tc.Pols {
				exp[i] = p.Stub()
			}
			require.ElementsMatch(t, exp, resp.Policies)
		})
	}
}

func TestScalingEndpoint_List_MultiNamespace_ACL(t *testing.T) {
	t.Parallel()
	s1, root, cleanupS1 := TestACLServer(t, nil)
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	state := s1.fsm.State()

	ns1 := mock.Namespace()
	ns2 := mock.Namespace()
	require.NoError(t, state.UpsertNamespaces(900, []*structs.Namespace{ns1, ns2}))

	ns1token := mock.CreatePolicyAndToken(t, state, 1001, "ns1",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilityListScalingPolicies}))
	ns1tokenViaJobs := mock.CreatePolicyAndToken(t, state, 1001, "ns1-read-job",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilityListJobs, acl.NamespaceCapabilityReadJob}))
	ns2token := mock.CreatePolicyAndToken(t, state, 1001, "ns2",
		mock.NamespacePolicy(ns2.Name, "", []string{acl.NamespaceCapabilityListScalingPolicies}))
	ns1BothToken := mock.CreatePolicyAndToken(t, state, 1001, "nsBoth",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilityListScalingPolicies})+
			mock.NamespacePolicy(ns2.Name, "", []string{acl.NamespaceCapabilityListScalingPolicies}))
	defaultListToken := mock.CreatePolicyAndToken(t, state, 1001, "default-list-job",
		mock.NamespacePolicy("default", "", []string{acl.NamespaceCapabilityListJobs}))

	// two namespaces
	require.NoError(t, state.UpsertNamespaces(900, []*structs.Namespace{ns1, ns2}))
	job1a, pol1a := mock.JobWithScalingPolicy()
	job1a.Namespace = ns1.Name
	pol1a.TargetTaskGroup(job1a, job1a.TaskGroups[0])
	require.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, job1a))
	job1b, pol1b := mock.JobWithScalingPolicy()
	job1b.Namespace = ns1.Name
	pol1b.TargetTaskGroup(job1b, job1b.TaskGroups[0])
	require.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, job1b))
	job2, pol2 := mock.JobWithScalingPolicy()
	job2.Namespace = ns2.Name
	pol2.TargetTaskGroup(job2, job2.TaskGroups[0])
	require.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, job2))

	cases := []struct {
		Label     string
		Namespace string
		Token     string
		Pols      []*structs.ScalingPolicy
		Error     bool
		Message   string
	}{
		{
			Label:     "all namespaces with sufficient token",
			Namespace: "*",
			Token:     ns1BothToken.SecretID,
			Pols:      []*structs.ScalingPolicy{pol1a, pol1b, pol2},
		},
		{
			Label:     "all namespaces with root token",
			Namespace: "*",
			Token:     root.SecretID,
			Pols:      []*structs.ScalingPolicy{pol1a, pol1b, pol2},
		},
		{
			Label:     "all namespaces with ns1 token",
			Namespace: "*",
			Token:     ns1token.SecretID,
			Pols:      []*structs.ScalingPolicy{pol1a, pol1b},
		},
		{
			Label:     "all namespaces with ns2 token",
			Namespace: "*",
			Token:     ns2token.SecretID,
			Pols:      []*structs.ScalingPolicy{pol2},
		},
		{
			Label:     "all namespaces with bad token",
			Namespace: "*",
			Token:     uuid.Generate(),
			Error:     true,
			Message:   structs.ErrTokenNotFound.Error(),
		},
		{
			Label:     "all namespaces with insufficient token",
			Namespace: "*",
			Pols:      []*structs.ScalingPolicy{},
			Token:     defaultListToken.SecretID,
		},
		{
			Label:     "ns1 with ns1 token",
			Namespace: ns1.Name,
			Token:     ns1token.SecretID,
			Pols:      []*structs.ScalingPolicy{pol1a, pol1b},
		},
		{
			Label:     "ns1 with ns1 read-job token",
			Namespace: ns1.Name,
			Token:     ns1tokenViaJobs.SecretID,
			Pols:      []*structs.ScalingPolicy{pol1a, pol1b},
		},
		{
			Label:     "ns1 with root token",
			Namespace: ns1.Name,
			Token:     root.SecretID,
			Pols:      []*structs.ScalingPolicy{pol1a, pol1b},
		},
		{
			Label:     "ns1 with ns2 token",
			Namespace: ns1.Name,
			Token:     ns2token.SecretID,
			Error:     true,
		},
		{
			Label:     "ns1 with invalid token",
			Namespace: ns1.Name,
			Token:     uuid.Generate(),
			Error:     true,
			Message:   structs.ErrTokenNotFound.Error(),
		},
		{
			Label:     "bad namespace, root token",
			Namespace: uuid.Generate(),
			Token:     root.SecretID,
			Pols:      []*structs.ScalingPolicy{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			// wrong namespace still works in the absence of ACLs
			var resp structs.ScalingPolicyListResponse
			err := msgpackrpc.CallWithCodec(
				codec,
				"Scaling.ListPolicies",
				&structs.ScalingPolicyListRequest{
					QueryOptions: structs.QueryOptions{
						Region:    "global",
						Namespace: tc.Namespace,
						AuthToken: tc.Token,
					},
				}, &resp)
			if tc.Error {
				require.Error(t, err)
				if tc.Message != "" {
					require.Equal(t, err.Error(), tc.Message)
				} else {
					require.Equal(t, err.Error(), structs.ErrPermissionDenied.Error())
				}
			} else {
				require.NoError(t, err)
				exp := make([]*structs.ScalingPolicyListStub, len(tc.Pols))
				for i, p := range tc.Pols {
					exp[i] = p.Stub()
					exp[i].CreateIndex = 900
					exp[i].ModifyIndex = 900
				}
				require.ElementsMatch(t, exp, resp.Policies)
			}
		})
	}
}
