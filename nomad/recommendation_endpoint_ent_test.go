//go:build ent
// +build ent

package nomad

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/hashicorp/go-memdb"
	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/acl"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/shoenig/test/must"
)

func TestRecommendationEndpoint_GetRecommendation(t *testing.T) {
	ci.Parallel(t)

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Create a recommendation
	ns := mock.Namespace()
	must.NoError(t, s1.State().UpsertNamespaces(900, []*structs.Namespace{ns}))
	job := mock.Job()
	job.Namespace = ns.Name
	job.Version = 5
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 905, nil, job))
	rec := mock.Recommendation(job)
	must.NoError(t, s1.State().UpsertRecommendation(910, rec))

	cases := []struct {
		Label     string
		ID        string
		Namespace string
		Missing   bool
	}{
		{
			Label:     "missing rec",
			ID:        uuid.Generate(),
			Namespace: "",
			Missing:   true,
		},
		{
			Label:     "wrong namespace w/o ACLs",
			ID:        rec.ID,
			Namespace: "default",
			Missing:   false,
		},
		{
			Label:     "right namespace",
			ID:        rec.ID,
			Namespace: ns.Name,
			Missing:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			// wrong namespace still works in the absence of ACLs
			var resp structs.SingleRecommendationResponse
			err := msgpackrpc.CallWithCodec(
				codec,
				"Recommendation.GetRecommendation",
				&structs.RecommendationSpecificRequest{
					RecommendationID: tc.ID,
					QueryOptions: structs.QueryOptions{
						Region:    "global",
						Namespace: tc.Namespace,
					},
				}, &resp)
			must.NoError(t, err)
			if tc.Missing {
				must.Nil(t, resp.Recommendation)
			} else {
				must.NotNil(t, resp.Recommendation)
				must.Eq(t, resp.Recommendation.ID, rec.ID)
			}
		})
	}
}

func TestRecommendationEndpoint_GetRecommendation_ACL(t *testing.T) {
	ci.Parallel(t)
	s1, root, cleanupS1 := TestACLServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	state := s1.fsm.State()

	ns1 := mock.Namespace()
	ns2 := mock.Namespace()
	must.NoError(t, state.UpsertNamespaces(900, []*structs.Namespace{ns1, ns2}))

	ns1token := mock.CreatePolicyAndToken(t, state, 1001, "ns1",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilityReadJob}))
	ns2token := mock.CreatePolicyAndToken(t, state, 1003, "ns2",
		mock.NamespacePolicy(ns2.Name, "", []string{acl.NamespaceCapabilityReadJob}))

	job := mock.Job()
	job.Namespace = ns1.Name
	job.Version = 5
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 905, nil, job))
	rec := mock.Recommendation(job)
	must.NoError(t, state.UpsertRecommendation(910, rec))

	cases := []struct {
		Label     string
		Namespace string
		Token     string
		Error     bool
		Found     bool
	}{
		{
			Label:     "cross namespace",
			Namespace: ns2.Name,
			Token:     ns2token.SecretID,
			Error:     false,
			Found:     false,
		},
		{
			Label:     "no token",
			Namespace: ns1.Name,
			Token:     "",
			Error:     true,
			Found:     false,
		},
		{
			Label:     "invalid token",
			Namespace: ns1.Name,
			Token:     ns2token.SecretID,
			Error:     true,
			Found:     false,
		},
		{
			Label:     "valid token",
			Namespace: ns1.Name,
			Token:     ns1token.SecretID,
			Error:     false,
			Found:     true,
		},
		{
			Label:     "mgmt token matching namespace",
			Namespace: ns1.Name,
			Token:     root.SecretID,
			Error:     false,
			Found:     true,
		},
		{
			Label:     "mgmt token cross namespace",
			Namespace: ns2.Name,
			Token:     root.SecretID,
			Error:     false,
			Found:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			get := &structs.RecommendationSpecificRequest{
				RecommendationID: rec.ID,
				QueryOptions: structs.QueryOptions{
					AuthToken: tc.Token,
					Namespace: tc.Namespace,
					Region:    "global",
				},
			}
			var resp structs.SingleRecommendationResponse
			err := msgpackrpc.CallWithCodec(codec, "Recommendation.GetRecommendation",
				get, &resp)
			if tc.Error {
				must.Error(t, err)
				must.Eq(t, err.Error(), structs.ErrPermissionDenied.Error())
			} else {
				must.NoError(t, err)
			}
			must.Eq(t, tc.Found, resp.Recommendation != nil)
		})
	}
}

func TestRecommendationEndpoint_GetRecommendation_License(t *testing.T) {
	ci.Parallel(t)

	cases := []struct {
		Label   string
		License *nomadLicense.TestLicense
		Error   bool
	}{
		{
			Label:   "platform",
			Error:   true,
			License: nomadLicense.NewTestLicense(nomadLicense.TestPlatformFlags()),
		},
		{
			Label: "multicluster and efficiency module",
			Error: false,
			License: nomadLicense.NewTestLicense(map[string]interface{}{
				"modules": []interface{}{nomadLicense.ModuleMulticlusterAndEfficiency.String()},
			}),
		},
		{
			Label: "dynamic application sizing feature",
			Error: false,
			License: nomadLicense.NewTestLicense(map[string]interface{}{
				"modules": []interface{}{},
				"features": map[string]interface{}{
					"add": []string{nomadLicense.FeatureDynamicApplicationSizing.String()},
				},
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			s, cleanup := TestServer(t, func(c *Config) {
				c.LicenseConfig.LicenseEnvBytes = tc.License.Signed
			})
			defer cleanup()
			codec := rpcClient(t, s)
			state := s.fsm.State()
			job := mock.Job()
			must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 905, nil, job))
			rec := mock.Recommendation(job)
			must.NoError(t, state.UpsertRecommendation(910, rec))

			get := &structs.RecommendationSpecificRequest{
				RecommendationID: rec.ID,
				QueryOptions: structs.QueryOptions{
					Namespace: "default",
					Region:    "global",
				},
			}
			var resp structs.SingleRecommendationResponse
			err := msgpackrpc.CallWithCodec(codec, "Recommendation.GetRecommendation", get, &resp)
			if tc.Error {
				must.Error(t, err)
				must.Eq(t, `Feature "Dynamic Application Sizing" is unlicensed`, err.Error())
				must.Nil(t, resp.Recommendation)
			} else {
				must.NoError(t, err)
				must.NotNil(t, resp.Recommendation)
			}
		})
	}
}

func TestRecommendationEndpoint_GetRecommendation_Blocking(t *testing.T) {
	ci.Parallel(t)

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)
	state := s1.fsm.State()

	// Create the deployments
	job := mock.Job()
	rec1 := mock.Recommendation(job)
	rec2 := mock.Recommendation(job)
	rec2.Target(
		job.TaskGroups[0].Name,
		job.TaskGroups[0].Tasks[0].Name,
		"MemoryMB")

	must.Nil(t, state.UpsertJob(structs.MsgTypeTestSetup, 98, nil, job), must.Sprint("UpsertJob"))

	// Upsert a recommendation nil, we are not interested in first.
	time.AfterFunc(100*time.Millisecond, func() {
		must.Nil(t, state.UpsertRecommendation(100, rec1), must.Sprint("UpsertRecommendation"))
	})

	// Upsert another recommendation later which should trigger the watch.
	time.AfterFunc(200*time.Millisecond, func() {
		must.Nil(t, state.UpsertRecommendation(200, rec2), must.Sprint("UpsertRecommendation"))
	})

	// Lookup the recommendations
	get := &structs.RecommendationSpecificRequest{
		RecommendationID: rec2.ID,
		QueryOptions: structs.QueryOptions{
			Region:        "global",
			Namespace:     structs.DefaultNamespace,
			MinQueryIndex: 150,
		},
	}
	start := time.Now()
	var resp structs.SingleRecommendationResponse
	must.Nil(t, msgpackrpc.CallWithCodec(codec, "Recommendation.GetRecommendation", get, &resp), must.Sprint("RPC"))
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Fatalf("should block (returned in %s) %#v", elapsed, resp)
	}
	must.Eq(t, 200, resp.Index, must.Sprint("resp.Index"))
	must.Eq(t, rec2.ID, resp.Recommendation.ID)
}

func TestRecommendationEndpoint_ListRecommendations(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)
	state := s1.State()

	// two namespaces
	ns1 := mock.Namespace()
	ns2 := mock.Namespace()
	must.NoError(t, state.UpsertNamespaces(900, []*structs.Namespace{ns1, ns2}))
	job1a := mock.Job()
	job1a.Namespace = ns1.Name
	job1a.TaskGroups = append(job1a.TaskGroups, job1a.TaskGroups[0].Copy())
	job1a.TaskGroups[1].Name = "second group"
	job1a.TaskGroups[1].Tasks = append(job1a.TaskGroups[1].Tasks, job1a.TaskGroups[1].Tasks[0].Copy())
	job1a.TaskGroups[1].Tasks[1].Name = "second task"
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 901, nil, job1a))
	rec1a := mock.Recommendation(job1a)
	rec1a.ID = "aa" + rec1a.ID[2:]
	must.NoError(t, state.UpsertRecommendation(901, rec1a))
	rec1a2 := mock.Recommendation(job1a)
	rec1a2.ID = "bb" + rec1a2.ID[2:]
	rec1a2.Target(job1a.TaskGroups[1].Name, job1a.TaskGroups[1].Tasks[0].Name, "CPU")
	must.NoError(t, state.UpsertRecommendation(901, rec1a2))
	rec1a22 := mock.Recommendation(job1a)
	rec1a22.ID = "cc" + rec1a22.ID[2:]
	rec1a22.Target(job1a.TaskGroups[1].Name, job1a.TaskGroups[1].Tasks[1].Name, "CPU")
	must.NoError(t, state.UpsertRecommendation(901, rec1a22))
	job1b := mock.Job()
	job1b.Namespace = ns1.Name
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 901, nil, job1b))
	rec1b := mock.Recommendation(job1b)
	rec1b.ID = "dd" + rec1b.ID[2:]
	must.NoError(t, state.UpsertRecommendation(901, rec1b))
	job2 := mock.Job()
	job2.Namespace = ns2.Name
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 902, nil, job2))
	rec2 := mock.Recommendation(job2)
	rec2.ID = "aa" + rec2.ID[2:]
	must.NoError(t, state.UpsertRecommendation(902, rec2))

	cases := []struct {
		Label     string
		Namespace string
		Prefix    string
		Job       string
		Group     string
		Task      string
		Recs      []*structs.Recommendation
	}{
		{
			Label:     "all namespaces",
			Namespace: "*",
			Recs:      []*structs.Recommendation{rec1a, rec1a2, rec1a22, rec1b, rec2},
		},
		{
			Label:     "all namespaces with prefix",
			Namespace: "*",
			Prefix:    rec1a.ID[0:2],
			Recs:      []*structs.Recommendation{rec1a, rec2},
		},
		{
			Label:     "all namespaces with non-matching prefix",
			Namespace: "*",
			Prefix:    "00",
			Recs:      []*structs.Recommendation{},
		},
		{
			Label:     "ns1",
			Namespace: ns1.Name,
			Recs:      []*structs.Recommendation{rec1a, rec1a2, rec1a22, rec1b},
		},
		{
			Label:     "ns1 with prefix",
			Namespace: ns1.Name,
			Prefix:    rec1a.ID[0:2],
			Recs:      []*structs.Recommendation{rec1a},
		},
		{
			Label:     "ns2",
			Namespace: ns2.Name,
			Recs:      []*structs.Recommendation{rec2},
		},
		{
			Label:     "bad namespace",
			Namespace: uuid.Generate(),
			Recs:      []*structs.Recommendation{},
		},
		{
			Label:     "job level with multiple",
			Namespace: ns1.Name,
			Job:       job1a.ID,
			Recs:      []*structs.Recommendation{rec1a, rec1a2, rec1a22},
		},
		{
			Label:     "job level with single",
			Namespace: ns2.Name,
			Job:       job2.ID,
			Recs:      []*structs.Recommendation{rec2},
		},
		{
			Label:     "job level for missing job",
			Namespace: ns1.Name,
			Job:       "missing job",
			Recs:      []*structs.Recommendation{},
		},
		{
			Label:     "group level 1",
			Namespace: ns1.Name,
			Job:       job1a.ID,
			Group:     job1a.TaskGroups[0].Name,
			Recs:      []*structs.Recommendation{rec1a},
		},
		{
			Label:     "group level 2",
			Namespace: ns1.Name,
			Job:       job1a.ID,
			Group:     job1a.TaskGroups[1].Name,
			Recs:      []*structs.Recommendation{rec1a2, rec1a22},
		},
		{
			Label:     "group level for missing group",
			Namespace: ns1.Name,
			Job:       job1a.ID,
			Group:     "missing group",
			Recs:      []*structs.Recommendation{},
		},
		{
			Label:     "task level 1",
			Namespace: ns1.Name,
			Job:       job1a.ID,
			Group:     job1a.TaskGroups[0].Name,
			Task:      job1a.TaskGroups[0].Tasks[0].Name,
			Recs:      []*structs.Recommendation{rec1a},
		},
		{
			Label:     "task level 2",
			Namespace: ns1.Name,
			Job:       job1a.ID,
			Group:     job1a.TaskGroups[1].Name,
			Task:      job1a.TaskGroups[1].Tasks[1].Name,
			Recs:      []*structs.Recommendation{rec1a22},
		},
		{
			Label:     "task level for missing task",
			Namespace: ns1.Name,
			Job:       job1a.ID,
			Group:     job1a.TaskGroups[1].Name,
			Task:      "missing task",
			Recs:      []*structs.Recommendation{},
		},
	}

	sortRecsById := func(slice []*structs.Recommendation) {
		sort.Slice(slice, func(i int, j int) bool {
			return slice[i].ID < slice[j].ID
		})
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			// wrong namespace still works in the absence of ACLs
			var resp structs.RecommendationListResponse
			err := msgpackrpc.CallWithCodec(
				codec,
				"Recommendation.ListRecommendations",
				&structs.RecommendationListRequest{
					JobID: tc.Job,
					Group: tc.Group,
					Task:  tc.Task,
					QueryOptions: structs.QueryOptions{
						Namespace: tc.Namespace,
						Prefix:    tc.Prefix,
						Region:    s1.Region(),
					},
				}, &resp)
			must.NoError(t, err)
			sortRecsById(tc.Recs)
			sortRecsById(resp.Recommendations)
			must.Eq(t, tc.Recs, resp.Recommendations)
		})
	}
}

func TestRecommendationEndpoint_ListRecommendations_ACL(t *testing.T) {
	ci.Parallel(t)
	s1, root, cleanupS1 := TestACLServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	state := s1.fsm.State()

	ns1 := mock.Namespace()
	ns2 := mock.Namespace()
	must.NoError(t, state.UpsertNamespaces(900, []*structs.Namespace{ns1, ns2}))

	ns1token := mock.CreatePolicyAndToken(t, state, 1001, "ns1",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilityReadJob}))
	ns1tokenSubmitRec := mock.CreatePolicyAndToken(t, state, 1001, "ns1-submit-rec",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilitySubmitRecommendation}))
	ns1tokenSubmitJob := mock.CreatePolicyAndToken(t, state, 1001, "ns1-submit-job",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilitySubmitJob}))
	ns2token := mock.CreatePolicyAndToken(t, state, 1001, "ns2",
		mock.NamespacePolicy(ns2.Name, "", []string{acl.NamespaceCapabilityReadJob}))
	ns1BothToken := mock.CreatePolicyAndToken(t, state, 1001, "nsBoth",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilityReadJob})+
			mock.NamespacePolicy(ns2.Name, "", []string{acl.NamespaceCapabilityReadJob}))
	defaultReadToken := mock.CreatePolicyAndToken(t, state, 1001, "default-read-job",
		mock.NamespacePolicy("default", "", []string{acl.NamespaceCapabilityReadJob}))

	// two namespaces
	must.NoError(t, state.UpsertNamespaces(900, []*structs.Namespace{ns1, ns2}))
	job1a := mock.Job()
	job1a.Namespace = ns1.Name
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 901, nil, job1a))
	rec1a := mock.Recommendation(job1a)
	must.NoError(t, state.UpsertRecommendation(901, rec1a))
	job1b := mock.Job()
	job1b.Namespace = ns1.Name
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 901, nil, job1b))
	rec1b := mock.Recommendation(job1b)
	must.NoError(t, state.UpsertRecommendation(901, rec1b))
	job2 := mock.Job()
	job2.Namespace = ns2.Name
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 902, nil, job2))
	rec2 := mock.Recommendation(job2)
	must.NoError(t, state.UpsertRecommendation(902, rec2))

	cases := []struct {
		Label     string
		Namespace string
		Token     string
		Recs      []*structs.Recommendation
		Error     bool
		Message   string
	}{
		{
			Label:     "all namespaces with sufficient token",
			Namespace: "*",
			Token:     ns1BothToken.SecretID,
			Recs:      []*structs.Recommendation{rec1a, rec1b, rec2},
		},
		{
			Label:     "all namespaces with root token",
			Namespace: "*",
			Token:     root.SecretID,
			Recs:      []*structs.Recommendation{rec1a, rec1b, rec2},
		},
		{
			Label:     "all namespaces with ns1 token",
			Namespace: "*",
			Token:     ns1token.SecretID,
			Recs:      []*structs.Recommendation{rec1a, rec1b},
		},
		{
			Label:     "all namespaces with ns2 token",
			Namespace: "*",
			Token:     ns2token.SecretID,
			Recs:      []*structs.Recommendation{rec2},
		},
		{
			Label:     "all namespaces with bad token",
			Namespace: "*",
			Token:     uuid.Generate(),
			Error:     true,
			Message:   structs.ErrPermissionDenied.Error(),
		},
		{
			Label:     "all namespaces with insufficient token",
			Namespace: "*",
			Recs:      []*structs.Recommendation{},
			Token:     defaultReadToken.SecretID,
		},
		{
			Label:     "ns1 with ns1 read-job token",
			Namespace: ns1.Name,
			Token:     ns1token.SecretID,
			Recs:      []*structs.Recommendation{rec1a, rec1b},
		},
		{
			Label:     "ns1 with ns1 submit-rec token",
			Namespace: ns1.Name,
			Token:     ns1tokenSubmitRec.SecretID,
			Recs:      []*structs.Recommendation{rec1a, rec1b},
		},
		{
			Label:     "ns1 with ns1 submit-job token",
			Namespace: ns1.Name,
			Token:     ns1tokenSubmitJob.SecretID,
			Recs:      []*structs.Recommendation{rec1a, rec1b},
		},
		{
			Label:     "ns1 with root token",
			Namespace: ns1.Name,
			Token:     root.SecretID,
			Recs:      []*structs.Recommendation{rec1a, rec1b},
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
			Message:   structs.ErrPermissionDenied.Error(),
		},
		{
			Label:     "bad namespace, root token",
			Namespace: uuid.Generate(),
			Token:     root.SecretID,
			Recs:      []*structs.Recommendation{},
		},
	}

	sortRecsById := func(slice []*structs.Recommendation) {
		sort.Slice(slice, func(i int, j int) bool {
			return slice[i].ID < slice[j].ID
		})
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			// wrong namespace still works in the absence of ACLs
			var resp structs.RecommendationListResponse
			err := msgpackrpc.CallWithCodec(
				codec,
				"Recommendation.ListRecommendations",
				&structs.RecommendationListRequest{
					QueryOptions: structs.QueryOptions{
						Namespace: tc.Namespace,
						AuthToken: tc.Token,
						Region:    s1.Region(),
					},
				}, &resp)
			if tc.Error {
				must.Error(t, err)
				if tc.Message != "" {
					must.Eq(t, err.Error(), tc.Message)
				} else {
					must.Eq(t, err.Error(), structs.ErrPermissionDenied.Error())
				}
			} else {
				must.NoError(t, err)
				sortRecsById(tc.Recs)
				sortRecsById(resp.Recommendations)
				must.Eq(t, tc.Recs, resp.Recommendations)
			}
		})
	}
}

func TestRecommendationEndpoint_ListRecommendations_License(t *testing.T) {
	ci.Parallel(t)

	cases := []struct {
		Label   string
		License *nomadLicense.TestLicense
		Error   bool
	}{
		{
			Label:   "platform",
			Error:   true,
			License: nomadLicense.NewTestLicense(nomadLicense.TestPlatformFlags()),
		},
		{
			Label: "multicluster and efficiency module",
			Error: false,
			License: nomadLicense.NewTestLicense(map[string]interface{}{
				"modules": []interface{}{nomadLicense.ModuleMulticlusterAndEfficiency.String()},
			}),
		},
		{
			Label: "dynamic application sizing feature",
			Error: false,
			License: nomadLicense.NewTestLicense(map[string]interface{}{
				"modules": []interface{}{},
				"features": map[string]interface{}{
					"add": []string{nomadLicense.FeatureDynamicApplicationSizing.String()},
				},
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			s, cleanup := TestServer(t, func(c *Config) {
				c.LicenseConfig.LicenseEnvBytes = tc.License.Signed
			})
			defer cleanup()
			codec := rpcClient(t, s)
			state := s.fsm.State()
			job := mock.Job()
			must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 905, nil, job))
			rec := mock.Recommendation(job)
			must.NoError(t, state.UpsertRecommendation(910, rec))

			get := &structs.RecommendationListRequest{
				QueryOptions: structs.QueryOptions{
					Namespace: "default",
					Region:    "global",
				},
			}
			var resp structs.RecommendationListResponse
			err := msgpackrpc.CallWithCodec(codec, "Recommendation.ListRecommendations", get, &resp)
			if tc.Error {
				must.Error(t, err)
				must.Eq(t, `Feature "Dynamic Application Sizing" is unlicensed`, err.Error())
				must.SliceEmpty(t, resp.Recommendations)
			} else {
				must.NoError(t, err)
				must.Len(t, 1, resp.Recommendations)
				must.Eq(t, rec.ID, resp.Recommendations[0].ID)
			}
		})
	}
}

func TestRecommendationEndpoint_ListRecommendations_Blocking(t *testing.T) {
	ci.Parallel(t)

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)
	state := s1.fsm.State()

	// Create the deployments
	job := mock.Job()
	rec1 := mock.Recommendation(job)
	rec2 := mock.Recommendation(job)
	rec2.Target(
		job.TaskGroups[0].Name,
		job.TaskGroups[0].Tasks[0].Name,
		"MemoryMB")

	must.Nil(t, state.UpsertJob(structs.MsgTypeTestSetup, 98, nil, job), must.Sprint("UpsertJob"))

	// Upsert a recommendation nil, we are not interested in first.
	time.AfterFunc(100*time.Millisecond, func() {
		must.Nil(t, state.UpsertRecommendation(100, rec1), must.Sprint("UpsertRecommendation"))
	})

	// Upsert another recommendation later which should trigger the watch.
	time.AfterFunc(200*time.Millisecond, func() {
		must.Nil(t, state.UpsertRecommendation(200, rec2), must.Sprint("UpsertRecommendation"))
	})

	// Lookup the recommendations
	get := &structs.RecommendationListRequest{
		QueryOptions: structs.QueryOptions{
			Region:        "global",
			Namespace:     structs.DefaultNamespace,
			MinQueryIndex: 150,
		},
	}
	start := time.Now()
	var resp structs.RecommendationListResponse
	must.Nil(t, msgpackrpc.CallWithCodec(codec, "Recommendation.ListRecommendations", get, &resp), must.Sprint("RPC"))
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Fatalf("should block (returned in %s) %#v", elapsed, resp)
	}
	must.Eq(t, 200, resp.Index, must.Sprint("resp.Index"))
	must.Len(t, 2, resp.Recommendations)
}

func TestRecommendationEndpoint_Upsert(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Create a recommendation
	job := mock.Job()
	job.Version = 5
	rec := mock.Recommendation(job)
	rec.Current = 0
	req := &structs.RecommendationUpsertRequest{
		Recommendation: rec,
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}

	now := time.Now().Unix()
	var resp structs.SingleRecommendationResponse
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
	err := msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req, &resp)
	must.NoError(t, err)

	iter, err := s1.State().Recommendations(nil)
	recs := make([]*structs.Recommendation, 0)
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		recs = append(recs, raw.(*structs.Recommendation))
	}
	must.Len(t, 1, recs)
	must.Eq(t, resp.Recommendation.ID, recs[0].ID)
	must.Eq(t, job.Version, resp.Recommendation.JobVersion)
	must.Eq(t, job.TaskGroups[0].Tasks[0].Resources.CPU, recs[0].Current)
	must.GreaterEq(t, now, resp.Recommendation.SubmitTime)
}

func TestRecommendationEndpoint_Upsert_License(t *testing.T) {
	ci.Parallel(t)

	cases := []struct {
		Label   string
		License *nomadLicense.TestLicense
		Error   bool
	}{
		{
			Label:   "platform",
			Error:   true,
			License: nomadLicense.NewTestLicense(nomadLicense.TestPlatformFlags()),
		},
		{
			Label: "multicluster and efficiency module",
			Error: false,
			License: nomadLicense.NewTestLicense(map[string]interface{}{
				"modules": []interface{}{nomadLicense.ModuleMulticlusterAndEfficiency.String()},
			}),
		},
		{
			Label: "dynamic application sizing feature",
			Error: false,
			License: nomadLicense.NewTestLicense(map[string]interface{}{
				"modules": []interface{}{},
				"features": map[string]interface{}{
					"add": []string{nomadLicense.FeatureDynamicApplicationSizing.String()},
				},
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			s, cleanup := TestServer(t, func(c *Config) {
				c.LicenseConfig.LicenseEnvBytes = tc.License.Signed
			})
			defer cleanup()
			codec := rpcClient(t, s)
			state := s.fsm.State()
			job := mock.Job()
			must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 905, nil, job))
			rec := mock.Recommendation(job)

			req := &structs.RecommendationUpsertRequest{
				Recommendation: rec,
				WriteRequest: structs.WriteRequest{
					Region: "global",
				},
			}
			var resp structs.SingleRecommendationResponse
			err := msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req, &resp)

			if tc.Error {
				must.Error(t, err)
				must.Eq(t, `Feature "Dynamic Application Sizing" is unlicensed`, err.Error())
				must.Nil(t, resp.Recommendation)
			} else {
				must.NoError(t, err)
				must.NotNil(t, resp.Recommendation)
			}
		})
	}
}

func TestRecommendationEndpoint_Upsert_NamespacePrecendence(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	state := s1.fsm.State()

	ns1 := mock.Namespace()
	ns2 := mock.Namespace()
	must.NoError(t, state.UpsertNamespaces(900, []*structs.Namespace{ns1, ns2}))

	cases := []struct {
		Label     string
		RequestNS string
		PayloadNS string
		Error     bool
		Message   string
		ResultNS  string
	}{
		{
			Label:     "cross namespace is an error",
			RequestNS: ns1.Name,
			PayloadNS: ns2.Name,
			Error:     true,
			Message:   "mismatched request namespace",
		},
		{
			Label:     "no request namespace",
			RequestNS: "",
			PayloadNS: ns2.Name,
			Error:     false,
			ResultNS:  ns2.Name,
		},
		{
			Label:     "matching namespaces",
			RequestNS: ns2.Name,
			PayloadNS: ns2.Name,
			Error:     false,
			ResultNS:  ns2.Name,
		},
		{
			Label:     "no rec namespace",
			RequestNS: ns2.Name,
			PayloadNS: "",
			Error:     false,
			ResultNS:  ns2.Name,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			job := mock.Job()
			if tc.Error == false {
				job.Namespace = tc.ResultNS
			}
			rec := mock.Recommendation(job)
			rec.Namespace = tc.PayloadNS
			req := &structs.RecommendationUpsertRequest{
				Recommendation: rec,
				WriteRequest: structs.WriteRequest{
					Region:    "global",
					Namespace: tc.RequestNS,
				},
			}
			var resp structs.SingleRecommendationResponse
			must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
			err := msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req, &resp)
			if tc.Error {
				must.Error(t, err)
				must.StrContains(t, err.Error(), tc.Message)
			} else {
				must.NoError(t, err)
				must.Eq(t, tc.ResultNS, resp.Recommendation.Namespace)
			}
		})
	}
}

func TestRecommendationEndpoint_Upsert_ACL(t *testing.T) {
	ci.Parallel(t)
	s1, root, cleanupS1 := TestACLServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	state := s1.fsm.State()

	ns1 := mock.Namespace()
	ns2 := mock.Namespace()
	must.NoError(t, state.UpsertNamespaces(900, []*structs.Namespace{ns1, ns2}))

	ns1token_readJob := mock.CreatePolicyAndToken(t, state, 1000, "ns1-read",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilityReadJob}))
	ns1token := mock.CreatePolicyAndToken(t, state, 1001, "ns1",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilitySubmitRecommendation}))
	ns1token_submitJob := mock.CreatePolicyAndToken(t, state, 1001, "ns1-submit-job",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilitySubmitJob}))
	ns2token := mock.CreatePolicyAndToken(t, state, 1002, "ns2",
		mock.NamespacePolicy(ns2.Name, "", []string{acl.NamespaceCapabilitySubmitRecommendation}))

	cases := []struct {
		Label     string
		Namespace string
		Token     string
		Error     bool
		Message   string
	}{
		{
			Label:     "cross namespace",
			Namespace: ns2.Name,
			Token:     ns2token.SecretID,
			Error:     true,
			Message:   "mismatched request namespace",
		},
		{
			Label:     "no token",
			Namespace: ns1.Name,
			Token:     "",
			Error:     true,
			Message:   structs.ErrPermissionDenied.Error(),
		},
		{
			Label:     "token from wrong namespace",
			Namespace: ns1.Name,
			Token:     ns2token.SecretID,
			Error:     true,
			Message:   structs.ErrPermissionDenied.Error(),
		},
		{
			Label:     "insufficient privileges",
			Namespace: ns1.Name,
			Token:     ns1token_readJob.SecretID,
			Error:     true,
			Message:   structs.ErrPermissionDenied.Error(),
		},
		{
			Label:     "valid submit-recommendation token",
			Namespace: ns1.Name,
			Token:     ns1token.SecretID,
			Error:     false,
		},
		{
			Label:     "valid submit-job token",
			Namespace: ns1.Name,
			Token:     ns1token_submitJob.SecretID,
			Error:     false,
		},
		{
			Label:     "mgmt token matching namespace",
			Namespace: ns1.Name,
			Token:     root.SecretID,
			Error:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			job := mock.Job()
			job.Namespace = ns1.Name
			rec := mock.Recommendation(job)
			req := &structs.RecommendationUpsertRequest{
				Recommendation: rec,
				WriteRequest: structs.WriteRequest{
					Region:    "global",
					Namespace: tc.Namespace,
					AuthToken: tc.Token,
				},
			}
			var resp structs.SingleRecommendationResponse
			must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
			err := msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req, &resp)
			if tc.Error {
				must.Error(t, err)
				must.StrContains(t, err.Error(), tc.Message)
			} else {
				must.NoError(t, err)
			}
		})
	}
}

func TestRecommendationEndpoint_Upsert_TargetFailures(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Create a recommendation
	job := mock.Job()
	rec := mock.Recommendation(job)
	req := &structs.RecommendationUpsertRequest{
		Recommendation: rec,
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}

	// Should fail, because job doesn't exist
	var resp structs.SingleRecommendationResponse
	err := msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req, &resp)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "does not exist")

	// Should fail, because request Namespace does not match payload
	req.Namespace = "not-default"
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req, &resp)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "400")
	must.StrContains(t, err.Error(), "mismatched request namespace")

	// Create the job
	req.Namespace = req.Recommendation.Namespace
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))

	// Should fail because missing task group
	req.Recommendation.Target("wrong job", "web", "CPU")
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req, &resp)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "does not exist in job")

	// Should fail because missing task
	req.Recommendation.Target("web", "wrong task", "CPU")
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req, &resp)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "does not exist in group")

	// Should fail because bad resource
	req.Recommendation.Target("web", "web", "GPU")
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req, &resp)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "resource not supported")
}

func TestRecommendationEndpoint_Upsert_ExistingRecByID(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Create a recommendation
	job := mock.Job()
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))

	originalRec := mock.Recommendation(job)
	originalRec.Value = 500
	req := &structs.RecommendationUpsertRequest{
		Recommendation: originalRec,
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	var resp structs.SingleRecommendationResponse
	err := msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req, &resp)
	must.NoError(t, err)
	recs, err := s1.State().RecommendationsByJob(nil, job.Namespace, job.ID, nil)
	must.NoError(t, err)
	must.Len(t, 1, recs)
	must.Eq(t, recs[0].ID, resp.Recommendation.ID)
	originalRec = resp.Recommendation

	// Updated recommendation value
	recUpdate := originalRec.Copy()
	recUpdate.Value = 1000
	recUpdate.Meta["updated"] = true
	req.Recommendation = recUpdate

	// update should overwrite the existing recommendation
	var updatedResp structs.SingleRecommendationResponse
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req, &updatedResp)
	must.NoError(t, err)
	recs, err = s1.State().RecommendationsByJob(nil, job.Namespace, job.ID, nil)
	must.NoError(t, err)
	must.Len(t, 1, recs)
	must.Eq(t, originalRec.ID, recs[0].ID)
	must.Eq(t, 1000, recs[0].Value)
	must.Eq(t, true, recs[0].Meta["updated"])
	must.Eq(t, originalRec.ID, updatedResp.Recommendation.ID)
}

func TestRecommendationEndpoint_Upsert_ExistingByPath(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Create a recommendation
	job := mock.Job()
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))

	originalRec := mock.Recommendation(job)
	originalRec.Value = 500
	req := &structs.RecommendationUpsertRequest{
		Recommendation: originalRec,
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	var resp structs.SingleRecommendationResponse
	err := msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req, &resp)
	must.NoError(t, err)
	recs, err := s1.State().RecommendationsByJob(nil, job.Namespace, job.ID, nil)
	must.NoError(t, err)
	must.Len(t, 1, recs)
	must.Eq(t, recs[0].ID, resp.Recommendation.ID)
	originalRec = resp.Recommendation

	// Updated recommendation value
	recUpdate := originalRec.Copy()
	recUpdate.ID = ""
	recUpdate.Value = 1000
	recUpdate.Meta["updated"] = true
	req.Recommendation = recUpdate

	// update should overwrite the existing recommendation
	var updatedResp structs.SingleRecommendationResponse
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req, &updatedResp)
	must.NoError(t, err)
	recs, err = s1.State().RecommendationsByJob(nil, job.Namespace, job.ID, nil)
	must.NoError(t, err)
	must.Len(t, 1, recs)
	must.Eq(t, originalRec.ID, recs[0].ID)
	must.Eq(t, 1000, recs[0].Value)
	must.Eq(t, true, recs[0].Meta["updated"])
	must.Eq(t, originalRec.ID, updatedResp.Recommendation.ID)
}

func TestRecommendationEndpoint_Upsert_MultipleRecs(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Create a recommendation
	job := mock.Job()
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))

	rec1 := mock.Recommendation(job)
	rec1.Value = 500
	req1 := &structs.RecommendationUpsertRequest{
		Recommendation: rec1,
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	var resp1 structs.SingleRecommendationResponse
	err := msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req1, &resp1)
	must.NoError(t, err)
	recs, err := s1.State().RecommendationsByJob(nil, job.Namespace, job.ID, nil)
	must.NoError(t, err)
	must.Len(t, 1, recs)
	must.Eq(t, recs[0].ID, resp1.Recommendation.ID)

	rec2 := mock.Recommendation(job)
	rec2.Target("web", "web", "MemoryMB")
	rec2.Value = 1024
	req2 := &structs.RecommendationUpsertRequest{
		Recommendation: rec2,
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	var resp2 structs.SingleRecommendationResponse
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req2, &resp2)
	must.NoError(t, err)
	recs, err = s1.State().RecommendationsByJob(nil, job.Namespace, job.ID, nil)
	must.NoError(t, err)
	must.Len(t, 2, recs)
	sort.Slice(recs, func(i, j int) bool {
		return recs[i].ID < recs[j].ID
	})
	exp := []*structs.Recommendation{resp1.Recommendation, resp2.Recommendation}
	sort.Slice(exp, func(i, j int) bool {
		return exp[i].ID < exp[j].ID
	})
	must.True(t, reflect.DeepEqual(exp, recs))
}

func TestRecommendationEndpoint_Delete_SingleRec(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Create a recommendation
	job := mock.Job()
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))

	rec1 := mock.Recommendation(job)
	rec1.Value = 500
	req1 := &structs.RecommendationUpsertRequest{
		Recommendation: rec1,
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	var resp1 structs.SingleRecommendationResponse
	err := msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req1, &resp1)
	must.NoError(t, err)

	rec2 := mock.Recommendation(job)
	rec2.Target("web", "web", "MemoryMB")
	rec2.Value = 1024
	req2 := &structs.RecommendationUpsertRequest{
		Recommendation: rec2,
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	var resp2 structs.SingleRecommendationResponse
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req2, &resp2)
	must.NoError(t, err)

	var delResp structs.GenericResponse
	delReq := &structs.RecommendationDeleteRequest{
		Recommendations: []string{resp1.Recommendation.ID},
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.DeleteRecommendations", delReq, &delResp)
	must.NoError(t, err)

	iter, err := s1.State().Recommendations(nil)
	recs := make([]*structs.Recommendation, 0)
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		recs = append(recs, raw.(*structs.Recommendation))
	}
	must.Len(t, 1, recs)
	must.Eq(t, recs[0].ID, resp2.Recommendation.ID)
}

func TestRecommendationEndpoint_Delete_MultipleRecs(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	// Create a recommendation
	job := mock.Job()
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))

	rec1 := mock.Recommendation(job)
	rec1.Value = 500
	req1 := &structs.RecommendationUpsertRequest{
		Recommendation: rec1,
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	var resp1 structs.SingleRecommendationResponse
	err := msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req1, &resp1)
	must.NoError(t, err)

	rec2 := mock.Recommendation(job)
	rec2.Target("web", "web", "MemoryMB")
	rec2.Value = 1024
	req2 := &structs.RecommendationUpsertRequest{
		Recommendation: rec2,
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	var resp2 structs.SingleRecommendationResponse
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.UpsertRecommendation", req2, &resp2)
	must.NoError(t, err)

	var delResp structs.GenericResponse
	delReq := &structs.RecommendationDeleteRequest{
		Recommendations: []string{resp1.Recommendation.ID, resp2.Recommendation.ID},
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.DeleteRecommendations", delReq, &delResp)
	must.NoError(t, err)

	iter, err := s1.State().Recommendations(nil)
	recs := make([]*structs.Recommendation, 0)
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		recs = append(recs, raw.(*structs.Recommendation))
	}
	must.Len(t, 0, recs)
}

func TestRecommendationEndpoint_Delete_License(t *testing.T) {
	ci.Parallel(t)

	cases := []struct {
		Label   string
		License *nomadLicense.TestLicense
		Error   bool
	}{
		{
			Label:   "platform",
			Error:   true,
			License: nomadLicense.NewTestLicense(nomadLicense.TestPlatformFlags()),
		},
		{
			Label: "multicluster and efficiency module",
			Error: false,
			License: nomadLicense.NewTestLicense(map[string]interface{}{
				"modules": []interface{}{nomadLicense.ModuleMulticlusterAndEfficiency.String()},
			}),
		},
		{
			Label: "dynamic application sizing feature",
			Error: false,
			License: nomadLicense.NewTestLicense(map[string]interface{}{
				"modules": []interface{}{},
				"features": map[string]interface{}{
					"add": []string{nomadLicense.FeatureDynamicApplicationSizing.String()},
				},
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			s, cleanup := TestServer(t, func(c *Config) {
				c.LicenseConfig.LicenseEnvBytes = tc.License.Signed
			})
			defer cleanup()
			codec := rpcClient(t, s)
			state := s.fsm.State()

			job := mock.Job()
			must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
			rec := mock.Recommendation(job)
			must.NoError(t, state.UpsertRecommendation(950, rec))
			var delResp structs.GenericResponse
			delReq := &structs.RecommendationDeleteRequest{
				Recommendations: []string{rec.ID},
				WriteRequest: structs.WriteRequest{
					Region: "global",
				},
			}
			err := msgpackrpc.CallWithCodec(codec, "Recommendation.DeleteRecommendations", delReq, &delResp)
			if tc.Error {
				must.Error(t, err)
				must.Eq(t, `Feature "Dynamic Application Sizing" is unlicensed`, err.Error())
			} else {
				must.NoError(t, err)
			}
		})
	}
}

func TestRecommendationEndpoint_Delete_Errors(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	var delResp structs.GenericResponse
	delReq := &structs.RecommendationDeleteRequest{
		Recommendations: []string{uuid.Generate()},
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	err := msgpackrpc.CallWithCodec(codec, "Recommendation.DeleteRecommendations", delReq, &delResp)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "does not exist")

	delReq.Recommendations = []string{}
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.DeleteRecommendations", delReq, &delResp)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "must specify at least one recommendation to delete")
}

func TestRecommendationEndpoint_Delete_ACL(t *testing.T) {
	ci.Parallel(t)
	s, root, cleanupS1 := TestACLServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
	})
	defer cleanupS1()
	codec := rpcClient(t, s)
	testutil.WaitForLeader(t, s.RPC)

	state := s.fsm.State()

	ns1 := mock.Namespace()
	ns2 := mock.Namespace()
	must.NoError(t, state.UpsertNamespaces(900, []*structs.Namespace{ns1, ns2}))

	ns1token_readJob := mock.CreatePolicyAndToken(t, state, 900, "ns1-read",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilityReadJob}))
	ns1token := mock.CreatePolicyAndToken(t, state, 901, "ns1",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilitySubmitRecommendation}))
	ns1token_submitJob := mock.CreatePolicyAndToken(t, state, 902, "ns1-submit-job",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilitySubmitJob}))
	ns2token := mock.CreatePolicyAndToken(t, state, 903, "ns2",
		mock.NamespacePolicy(ns2.Name, "", []string{acl.NamespaceCapabilitySubmitRecommendation}))
	nsBothToken := mock.CreatePolicyAndToken(t, state, 904, "nsBoth",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilitySubmitRecommendation})+
			mock.NamespacePolicy(ns2.Name, "", []string{acl.NamespaceCapabilitySubmitRecommendation}))
	nsBothToken_submitJob := mock.CreatePolicyAndToken(t, state, 905, "nsBoth-submit-job",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilitySubmitJob})+
			mock.NamespacePolicy(ns2.Name, "", []string{acl.NamespaceCapabilitySubmitJob}))

	job1 := mock.Job()
	job1.Namespace = ns1.Name
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 904, nil, job1))
	job2 := mock.Job()
	job2.Namespace = ns2.Name
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 905, nil, job2))
	rec1 := mock.Recommendation(job1)
	rec2 := mock.Recommendation(job2)

	cases := []struct {
		Label string
		Recs  []string
		Token string
		Error bool
	}{
		{
			Label: "no token",
			Recs:  []string{rec1.ID},
			Token: "",
			Error: true,
		},
		{
			Label: "token from different namespace",
			Recs:  []string{rec1.ID},
			Token: ns2token.SecretID,
			Error: true,
		},
		{
			Label: "insufficient privileges on namespace",
			Recs:  []string{rec1.ID},
			Token: ns1token_readJob.SecretID,
			Error: true,
		},
		{
			Label: "submit-recommendation on only one namespace",
			Recs:  []string{rec1.ID, rec2.ID},
			Token: ns1token.SecretID,
			Error: true,
		},
		{
			Label: "submit-job on only one namespace",
			Recs:  []string{rec1.ID, rec2.ID},
			Token: ns1token_submitJob.SecretID,
			Error: true,
		},
		{
			Label: "valid submit-recommendation token",
			Recs:  []string{rec1.ID},
			Token: ns1token.SecretID,
			Error: false,
		},
		{
			Label: "valid submit-job token",
			Recs:  []string{rec1.ID},
			Token: ns1token_submitJob.SecretID,
			Error: false,
		},
		{
			Label: "submit-rec on both namespaces",
			Recs:  []string{rec1.ID, rec2.ID},
			Token: nsBothToken.SecretID,
			Error: false,
		},
		{
			Label: "submit-job on both namespaces",
			Recs:  []string{rec1.ID, rec2.ID},
			Token: nsBothToken_submitJob.SecretID,
			Error: false,
		},
		{
			Label: "mgmt token can do anything",
			Recs:  []string{rec1.ID, rec2.ID},
			Token: root.SecretID,
			Error: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			// cleanup and recreate
			_ = state.DeleteRecommendations(1000, []string{rec1.ID, rec2.ID})
			must.NoError(t, state.UpsertRecommendation(1001, rec1))
			must.NoError(t, state.UpsertRecommendation(1002, rec2))

			delReq := structs.RecommendationDeleteRequest{
				Recommendations: tc.Recs,
				WriteRequest: structs.WriteRequest{
					Region:    "global",
					AuthToken: tc.Token,
				},
			}
			var delResp structs.GenericResponse
			err := msgpackrpc.CallWithCodec(codec, "Recommendation.DeleteRecommendations", delReq, &delResp)
			if tc.Error {
				must.Error(t, err)
				must.StrContains(t, err.Error(), structs.ErrPermissionDenied.Error())
			} else {
				must.NoError(t, err)
			}
		})
	}
}

func TestRecommendationEndpoint_Apply_SingleRec(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
		c.NumSchedulers = 0 // Prevent automatic dequeue
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	job := mock.Job()
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))

	rec1 := mock.Recommendation(job)
	must.NoError(t, s1.State().UpsertRecommendation(910, rec1))

	rec2 := mock.Recommendation(job)
	rec2.Target("web", "web", "MemoryMB")
	rec2.Value = job.TaskGroups[0].Tasks[0].Resources.MemoryMB * 2
	rec2.EnforceVersion = true
	must.NoError(t, s1.State().UpsertRecommendation(920, rec2))

	// set up watch set for job update on rec apply
	jobWatch := memdb.NewWatchSet()
	_, err := s1.State().JobByID(jobWatch, job.Namespace, job.ID)
	must.NoError(t, err)

	// set up watch for rec1, which will be deleted by the job update
	rec2Watch := memdb.NewWatchSet()
	_, err = s1.State().RecommendationByID(rec2Watch, rec2.ID)

	var applyResp structs.RecommendationApplyResponse
	applyReq := &structs.RecommendationApplyRequest{
		Recommendations: []string{rec1.ID},
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.ApplyRecommendations", applyReq, &applyResp)
	must.NoError(t, err)
	must.Len(t, 0, applyResp.Errors)
	must.Len(t, 1, applyResp.UpdatedJobs)
	must.Eq(t, job.Namespace, applyResp.UpdatedJobs[0].Namespace)
	must.Eq(t, job.ID, applyResp.UpdatedJobs[0].JobID)
	must.Eq(t, []string{rec1.ID}, applyResp.UpdatedJobs[0].Recommendations)

	must.False(t, jobWatch.Watch(time.After(100*time.Millisecond)))
	must.False(t, rec2Watch.Watch(time.After(100*time.Millisecond)))

	job, err = s1.State().JobByID(nil, job.Namespace, job.ID)
	must.NoError(t, err)
	must.NotNil(t, job)
	must.Eq(t, job.LookupTaskGroup(rec1.Group).LookupTask(rec1.Task).Resources.CPU, rec1.Value)
	must.Eq(t, job.ModifyIndex, applyResp.UpdatedJobs[0].JobModifyIndex)

	// rec1 was deleted during application, while rec2 was deleted because of the job update
	recs, err := s1.State().RecommendationsByJob(nil, job.Namespace, job.ID, nil)
	must.NoError(t, err)
	must.Len(t, 0, recs)
}

func TestRecommendationEndpoint_Apply_MultipleRecs(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
		c.NumSchedulers = 0 // Prevent automatic dequeue
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	job := mock.Job()
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))

	rec1 := mock.Recommendation(job)
	must.NoError(t, s1.State().UpsertRecommendation(910, rec1))

	rec2 := mock.Recommendation(job)
	rec2.Target("web", "web", "MemoryMB")
	rec2.Value = job.TaskGroups[0].Tasks[0].Resources.MemoryMB * 2
	must.NoError(t, s1.State().UpsertRecommendation(920, rec2))

	// set up watch set for job update on rec apply
	jobWatch := memdb.NewWatchSet()
	_, err := s1.State().JobByID(jobWatch, job.Namespace, job.ID)
	must.NoError(t, err)

	// set up watch for rec1, which will be deleted by the job update
	rec2Watch := memdb.NewWatchSet()
	_, err = s1.State().RecommendationByID(rec2Watch, rec2.ID)

	var applyResp structs.RecommendationApplyResponse
	applyReq := &structs.RecommendationApplyRequest{
		Recommendations: []string{rec1.ID, rec2.ID},
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.ApplyRecommendations", applyReq, &applyResp)
	must.NoError(t, err)
	must.Len(t, 0, applyResp.Errors)
	must.Len(t, 1, applyResp.UpdatedJobs)
	must.Eq(t, job.Namespace, applyResp.UpdatedJobs[0].Namespace)
	must.Eq(t, job.ID, applyResp.UpdatedJobs[0].JobID)
	must.Eq(t, []string{rec1.ID, rec2.ID}, applyResp.UpdatedJobs[0].Recommendations)

	must.False(t, jobWatch.Watch(time.After(100*time.Millisecond)))
	must.False(t, rec2Watch.Watch(time.After(100*time.Millisecond)))

	job, err = s1.State().JobByID(nil, job.Namespace, job.ID)
	must.NoError(t, err)
	must.NotNil(t, job)
	must.Eq(t, job.LookupTaskGroup(rec1.Group).LookupTask(rec1.Task).Resources.CPU, rec1.Value)
	must.Eq(t, job.LookupTaskGroup(rec1.Group).LookupTask(rec1.Task).Resources.MemoryMB, rec2.Value)
	must.Eq(t, job.ModifyIndex, applyResp.UpdatedJobs[0].JobModifyIndex)

	// both recommendations were deleted during update
	recs, err := s1.State().RecommendationsByJob(nil, job.Namespace, job.ID, nil)
	must.NoError(t, err)
	must.Len(t, 0, recs)
}

func TestRecommendationEndpoint_Apply_MultipleJobs(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
		c.NumSchedulers = 0 // Prevent automatic dequeue
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	job1 := mock.Job()
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 900, nil, job1))
	rec1 := mock.Recommendation(job1)
	must.NoError(t, s1.State().UpsertRecommendation(910, rec1))

	job2 := mock.Job()
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 901, nil, job2))
	rec2 := mock.Recommendation(job2)
	rec2.Target("web", "web", "MemoryMB")
	rec2.Value = job2.TaskGroups[0].Tasks[0].Resources.MemoryMB * 2
	must.NoError(t, s1.State().UpsertRecommendation(920, rec2))

	// set up watch set for job updates on rec apply
	job1Watch := memdb.NewWatchSet()
	_, err := s1.State().JobByID(job1Watch, job1.Namespace, job1.ID)
	must.NoError(t, err)
	job2Watch := memdb.NewWatchSet()
	_, err = s1.State().JobByID(job2Watch, job2.Namespace, job2.ID)
	must.NoError(t, err)

	var applyResp structs.RecommendationApplyResponse
	applyReq := &structs.RecommendationApplyRequest{
		Recommendations: []string{rec1.ID, rec2.ID},
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.ApplyRecommendations", applyReq, &applyResp)
	must.NoError(t, err)
	must.Len(t, 0, applyResp.Errors)
	must.Len(t, 2, applyResp.UpdatedJobs)

	jobs := []*structs.Job{job1, job2}
	sort.Slice(jobs, func(i int, j int) bool {
		return jobs[i].ID < jobs[j].ID
	})
	sort.Slice(applyResp.UpdatedJobs, func(i int, j int) bool {
		return applyResp.UpdatedJobs[i].JobID < applyResp.UpdatedJobs[j].JobID
	})

	must.Eq(t, jobs[0].Namespace, applyResp.UpdatedJobs[0].Namespace)
	must.Eq(t, jobs[0].ID, applyResp.UpdatedJobs[0].JobID)
	must.Eq(t, jobs[1].Namespace, applyResp.UpdatedJobs[1].Namespace)
	must.Eq(t, jobs[1].ID, applyResp.UpdatedJobs[1].JobID)

	must.False(t, job1Watch.Watch(time.After(100*time.Millisecond)))
	must.False(t, job2Watch.Watch(time.After(100*time.Millisecond)))

	job1, err = s1.State().JobByID(nil, job1.Namespace, job1.ID)
	must.NoError(t, err)
	must.NotNil(t, job1)
	must.Eq(t, job1.LookupTaskGroup(rec1.Group).LookupTask(rec1.Task).Resources.CPU, rec1.Value)

	job2, err = s1.State().JobByID(nil, job2.Namespace, job2.ID)
	must.NoError(t, err)
	must.NotNil(t, job2)
	must.Eq(t, job2.LookupTaskGroup(rec2.Group).LookupTask(rec2.Task).Resources.MemoryMB, rec2.Value)

	// all recommendations were deleted during update
	recs, err := s1.State().Recommendations(nil)
	must.NoError(t, err)
	must.Nil(t, recs.Next())
}

func TestRecommendationEndpoint_Apply_WithRegisterErrors(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
		c.NumSchedulers = 0 // Prevent automatic dequeue
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	job1 := mock.Job()
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 900, nil, job1))
	rec1 := mock.Recommendation(job1)
	must.NoError(t, s1.State().UpsertRecommendation(910, rec1))

	job2 := mock.Job()
	must.NoError(t, s1.State().UpsertJob(structs.MsgTypeTestSetup, 901, nil, job2))
	rec2 := mock.Recommendation(job2)
	rec2.Target("web", "web", "MemoryMB")
	rec2.Value = 0 // invalid value for MemoryDB, will trigger error on Job.Register
	must.NoError(t, s1.State().UpsertRecommendation(920, rec2))
	origMem2 := job2.LookupTaskGroup(rec2.Group).LookupTask(rec2.Task).Resources.MemoryMB

	// set up watch set for job updates on rec apply
	job1Watch := memdb.NewWatchSet()
	_, err := s1.State().JobByID(job1Watch, job1.Namespace, job1.ID)
	must.NoError(t, err)
	job2Watch := memdb.NewWatchSet()
	_, err = s1.State().JobByID(job2Watch, job2.Namespace, job2.ID)
	must.NoError(t, err)

	var applyResp structs.RecommendationApplyResponse
	applyReq := &structs.RecommendationApplyRequest{
		Recommendations: []string{rec1.ID, rec2.ID},
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.ApplyRecommendations", applyReq, &applyResp)
	must.NoError(t, err)
	must.Len(t, 1, applyResp.Errors)
	must.Len(t, 1, applyResp.UpdatedJobs)

	must.Eq(t, job1.Namespace, applyResp.UpdatedJobs[0].Namespace)
	must.Eq(t, job1.ID, applyResp.UpdatedJobs[0].JobID)
	must.Eq(t, job2.Namespace, applyResp.Errors[0].Namespace)
	must.Eq(t, job2.ID, applyResp.Errors[0].JobID)
	must.StrContains(t, applyResp.Errors[0].Error, "minimum MemoryMB value is 10")

	must.False(t, job1Watch.Watch(time.After(100*time.Millisecond)))
	must.True(t, job2Watch.Watch(time.After(100*time.Millisecond)))

	job1, err = s1.State().JobByID(nil, job1.Namespace, job1.ID)
	must.NoError(t, err)
	must.NotNil(t, job1)
	must.Eq(t, job1.LookupTaskGroup(rec1.Group).LookupTask(rec1.Task).Resources.CPU, rec1.Value)

	job2, err = s1.State().JobByID(nil, job2.Namespace, job2.ID)
	must.NoError(t, err)
	must.NotNil(t, job2)
	must.Eq(t, origMem2, job2.LookupTaskGroup(rec2.Group).LookupTask(rec2.Task).Resources.MemoryMB)

	// all recommendations were deleted during update
	recs, err := s1.State().Recommendations(nil)
	must.NoError(t, err)
	must.Nil(t, recs.Next())
}

func TestRecommendationEndpoint_Apply_License(t *testing.T) {
	ci.Parallel(t)

	cases := []struct {
		Label   string
		License *nomadLicense.TestLicense
		Error   bool
	}{
		{
			Label:   "platform",
			Error:   true,
			License: nomadLicense.NewTestLicense(nomadLicense.TestPlatformFlags()),
		},
		{
			Label: "multicluster and efficiency module",
			Error: false,
			License: nomadLicense.NewTestLicense(map[string]interface{}{
				"modules": []interface{}{nomadLicense.ModuleMulticlusterAndEfficiency.String()},
			}),
		},
		{
			Label: "dynamic application sizing feature",
			Error: false,
			License: nomadLicense.NewTestLicense(map[string]interface{}{
				"modules": []interface{}{},
				"features": map[string]interface{}{
					"add": []string{nomadLicense.FeatureDynamicApplicationSizing.String()},
				},
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			s, cleanup := TestServer(t, func(c *Config) {
				c.LicenseConfig.LicenseEnvBytes = tc.License.Signed
			})
			defer cleanup()
			codec := rpcClient(t, s)
			state := s.fsm.State()

			job := mock.Job()
			must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
			rec := mock.Recommendation(job)
			must.NoError(t, state.UpsertRecommendation(910, rec))
			var applyResp structs.RecommendationApplyResponse
			applyReq := &structs.RecommendationApplyRequest{
				Recommendations: []string{rec.ID},
				WriteRequest: structs.WriteRequest{
					Region: "global",
				},
			}
			err := msgpackrpc.CallWithCodec(codec, "Recommendation.ApplyRecommendations", applyReq, &applyResp)

			if tc.Error {
				must.Error(t, err)
				must.Eq(t, `Feature "Dynamic Application Sizing" is unlicensed`, err.Error())
			} else {
				must.NoError(t, err)
			}
		})
	}
}

func TestRecommendationEndpoint_Apply_Errors(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
		c.NumSchedulers = 0 // Prevent automatic dequeue
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)
	state := s1.State()

	job := mock.Job()
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
	rec := mock.Recommendation(job)
	must.NoError(t, state.UpsertRecommendation(900, rec))

	var applyResp structs.RecommendationApplyResponse
	applyReq := &structs.RecommendationApplyRequest{
		Recommendations: []string{},
		WriteRequest: structs.WriteRequest{
			Region: "global",
		},
	}
	err := msgpackrpc.CallWithCodec(codec, "Recommendation.ApplyRecommendations", applyReq, &applyResp)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "at least one recommendation")

	applyReq.Recommendations = []string{rec.ID}
	{
		// mutate rec in memory
		r, err := state.RecommendationByID(nil, rec.ID)
		must.NoError(t, err)
		r.JobID = "nonexistent job"
	}
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.ApplyRecommendations", applyReq, &applyResp)
	must.Error(t, err)
	must.StrContains(t, err.Error(), `job "nonexistent job" in namespace "default" does not exist`)
	// fix it
	rec.JobID = job.ID
	must.NoError(t, state.UpsertRecommendation(900, rec))

	applyReq.Recommendations = []string{uuid.Generate()}
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.ApplyRecommendations", applyReq, &applyResp)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "recommendation does not exist")

	applyReq.Recommendations = []string{rec.ID}
	rec.Target("bad group", job.TaskGroups[0].Tasks[0].Name, "CPU")
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.ApplyRecommendations", applyReq, &applyResp)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "task group does not exist in job")

	applyReq.Recommendations = []string{rec.ID}
	rec.Target(job.TaskGroups[0].Name, "bad task", "CPU")
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.ApplyRecommendations", applyReq, &applyResp)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "task does not exist in group")

	applyReq.Recommendations = []string{rec.ID}
	rec.Target(job.TaskGroups[0].Name, job.TaskGroups[0].Tasks[0].Name, "Bad Resource")
	err = msgpackrpc.CallWithCodec(codec, "Recommendation.ApplyRecommendations", applyReq, &applyResp)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "resource not valid")

}

func TestRecommendationEndpoint_Apply_ACL(t *testing.T) {
	ci.Parallel(t)
	s, root, cleanupS1 := TestACLServer(t, func(c *Config) {
		c.LicenseConfig.LicenseEnvBytes = licenseForMulticlusterEfficiency().Signed
		c.NumSchedulers = 0 // Prevent automatic dequeue
	})
	defer cleanupS1()
	codec := rpcClient(t, s)
	testutil.WaitForLeader(t, s.RPC)

	state := s.fsm.State()

	ns1 := mock.Namespace()
	ns2 := mock.Namespace()
	must.NoError(t, state.UpsertNamespaces(900, []*structs.Namespace{ns1, ns2}))

	ns1token_readJob := mock.CreatePolicyAndToken(t, state, 900, "ns1-read",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilityReadJob}))
	ns1token := mock.CreatePolicyAndToken(t, state, 901, "ns1",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilityReadJob, acl.NamespaceCapabilitySubmitJob}))
	ns2token := mock.CreatePolicyAndToken(t, state, 903, "ns2",
		mock.NamespacePolicy(ns2.Name, "", []string{acl.NamespaceCapabilityReadJob, acl.NamespaceCapabilitySubmitJob}))
	nsBothToken := mock.CreatePolicyAndToken(t, state, 903, "nsBoth",
		mock.NamespacePolicy(ns1.Name, "", []string{acl.NamespaceCapabilityReadJob, acl.NamespaceCapabilitySubmitJob})+
			mock.NamespacePolicy(ns2.Name, "", []string{acl.NamespaceCapabilityReadJob, acl.NamespaceCapabilitySubmitJob}))

	rec1ID := uuid.Generate()
	rec2ID := uuid.Generate()

	cases := []struct {
		Label   string
		Recs    []string
		Token   string
		Error   bool
		Message string
	}{
		{
			Label:   "no token",
			Recs:    []string{rec1ID},
			Token:   "",
			Error:   true,
			Message: "recommendation does not exist",
		},
		{
			Label:   "token from different namespace",
			Recs:    []string{rec1ID},
			Token:   ns2token.SecretID,
			Error:   true,
			Message: "recommendation does not exist",
		},
		{
			Label: "insufficient privileges on namespace",
			Recs:  []string{rec1ID},
			Token: ns1token_readJob.SecretID,
			Error: true,
		},
		{
			Label:   "submit-job on only one namespace",
			Recs:    []string{rec1ID, rec2ID},
			Token:   ns1token.SecretID,
			Error:   true,
			Message: "recommendation does not exist",
		},
		{
			Label: "valid submit-job token",
			Recs:  []string{rec1ID},
			Token: ns1token.SecretID,
			Error: false,
		},
		{
			Label: "submit-job on both namespaces",
			Recs:  []string{rec1ID, rec2ID},
			Token: nsBothToken.SecretID,
			Error: false,
		},
		{
			Label: "mgmt token can do anything",
			Recs:  []string{rec1ID, rec2ID},
			Token: root.SecretID,
			Error: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Label, func(t *testing.T) {
			// cleanup  and recreate
			job1 := mock.Job()
			job1.Namespace = ns1.Name
			must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 904, nil, job1))
			job2 := mock.Job()
			job2.Namespace = ns2.Name
			must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 905, nil, job2))
			rec1 := mock.Recommendation(job1)
			rec1.ID = rec1ID
			rec2 := mock.Recommendation(job2)
			rec2.ID = rec2ID
			_ = state.DeleteRecommendations(1000, []string{rec1.ID, rec2.ID})
			must.NoError(t, state.UpsertRecommendation(906, rec1))
			must.NoError(t, state.UpsertRecommendation(907, rec2))

			var applyResp structs.RecommendationApplyResponse
			applyReq := &structs.RecommendationApplyRequest{
				Recommendations: tc.Recs,
				WriteRequest: structs.WriteRequest{
					Region:    "global",
					AuthToken: tc.Token,
				},
			}
			err := msgpackrpc.CallWithCodec(codec, "Recommendation.ApplyRecommendations", applyReq, &applyResp)
			if tc.Error {
				must.Error(t, err)
				if tc.Message != "" {
					must.StrContains(t, err.Error(), tc.Message)
				} else {
					must.StrContains(t, err.Error(), structs.ErrPermissionDenied.Error())
				}
			} else {
				must.NoError(t, err)
			}
		})
	}
}
