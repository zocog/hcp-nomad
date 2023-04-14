//go:build ent
// +build ent

package agent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/shoenig/test/must"
)

func TestHTTP_RecommendationList(t *testing.T) {
	ci.Parallel(t)
	httpTest(t, nil, func(s *TestAgent) {
		names := []*structs.Namespace{}
		jobs := []*structs.Job{}
		recs := []*structs.Recommendation{}
		for i := 0; i < 3; i++ {
			// Create the job and recommendation
			ns := mock.Namespace()
			names = append(names, ns)
			nsArgs := structs.NamespaceUpsertRequest{
				Namespaces: []*structs.Namespace{ns},
				WriteRequest: structs.WriteRequest{
					Region: "global",
				},
			}
			must.NoError(t, s.Agent.RPC("Namespace.UpsertNamespaces", &nsArgs, &structs.GenericResponse{}))
			for j := 0; j < 2; j++ {
				job := mock.Job()
				jobs = append(jobs, job)
				job.Namespace = ns.Name
				jobArgs := structs.JobRegisterRequest{
					Job: job,
					WriteRequest: structs.WriteRequest{
						Region:    "global",
						Namespace: job.Namespace,
					},
				}
				must.NoError(t, s.Agent.RPC("Job.Register", &jobArgs, &structs.JobRegisterResponse{}))
				rec := mock.Recommendation(job)
				// set the prefixes for prefix search testing
				if j == 0 {
					rec.ID = "aa" + rec.ID[2:]
				} else {
					rec.ID = "bb" + rec.ID[2:]
				}
				// RPC Upsert doesn't let us specify the ID, so we'll add it directly to the state store
				s.Agent.server.State().UpsertRecommendation(1000, rec)
				rec.Current = job.TaskGroups[0].Tasks[0].Resources.CPU
				recs = append(recs, rec)
			}
		}

		cases := []struct {
			Label   string
			URL     string
			ExpRecs []*structs.Recommendation
		}{
			{
				Label:   "all namespaces",
				URL:     "/v1/recommendations?namespace=*",
				ExpRecs: append([]*structs.Recommendation{}, recs...),
			},
			{
				Label:   "single namespace",
				URL:     "/v1/recommendations?namespace=" + names[0].Name,
				ExpRecs: []*structs.Recommendation{recs[0], recs[1]},
			},
			{
				Label:   "all namespaces with prefix",
				URL:     "/v1/recommendations?namespace=*&prefix=aa",
				ExpRecs: []*structs.Recommendation{recs[0], recs[2], recs[4]},
			},
			{
				Label:   "single namespace with prefix",
				URL:     "/v1/recommendations?namespace=" + names[0].Name + "&prefix=aa",
				ExpRecs: []*structs.Recommendation{recs[0]},
			},
			{
				Label:   "single namespace with non-matching prefix",
				URL:     "/v1/recommendations?namespace=" + names[0].Name + "&prefix=cc",
				ExpRecs: []*structs.Recommendation{},
			},
			{
				Label:   "bad namespace",
				URL:     "/v1/recommendations?namespace=bad",
				ExpRecs: []*structs.Recommendation{},
			},
			{
				Label:   "single job",
				URL:     "/v1/recommendations?namespace=" + names[0].Name + "&job=" + jobs[1].ID,
				ExpRecs: []*structs.Recommendation{recs[1]},
			},
			{
				Label:   "bad job",
				URL:     "/v1/recommendations?namespace=" + names[0].Name + "&job=bad",
				ExpRecs: []*structs.Recommendation{},
			},
			{
				Label: "with group",
				URL: "/v1/recommendations?namespace=" + names[0].Name + "&job=" + jobs[1].ID +
					"&group=" + jobs[1].TaskGroups[0].Name,
				ExpRecs: []*structs.Recommendation{recs[1]},
			},
			{
				Label: "bad group",
				URL: "/v1/recommendations?namespace=" + names[0].Name + "&job=" + jobs[1].ID +
					"&group=bad",
				ExpRecs: []*structs.Recommendation{},
			},
			{
				Label: "with task",
				URL: "/v1/recommendations?namespace=" + names[0].Name + "&job=" + jobs[1].ID +
					"&group=" + jobs[1].TaskGroups[0].Name +
					"&task=" + jobs[1].TaskGroups[0].Tasks[0].Name,
				ExpRecs: []*structs.Recommendation{recs[1]},
			},
			{
				Label: "bad task",
				URL: "/v1/recommendations?namespace=" + names[0].Name + "&job=" + jobs[1].ID +
					"&group=" + jobs[1].TaskGroups[0].Name +
					"&task=bad",
				ExpRecs: []*structs.Recommendation{},
			},
		}

		for _, tc := range cases {
			t.Run(tc.Label, func(t *testing.T) {
				req, err := http.NewRequest("GET", tc.URL, nil)
				must.NoError(t, err)
				respW := httptest.NewRecorder()

				obj, err := s.Server.RecommendationsListRequest(respW, req)
				must.NoError(t, err)

				must.NotEq(t, respW.Header().Get("X-Nomad-Index"), "", must.Sprint("missing index response header"))
				must.NotEq(t, respW.Header().Get("X-Nomad-KnownLeader"), "", must.Sprint("missing known leader response header"))
				must.NotEq(t, respW.Header().Get("X-Nomad-LastContact"), "", must.Sprint("missing last contact response header"))

				actualRecs := obj.([]*structs.Recommendation)
				sort.Slice(actualRecs, func(i int, j int) bool {
					return actualRecs[i].ID < actualRecs[j].ID
				})
				sort.Slice(tc.ExpRecs, func(i int, j int) bool {
					return tc.ExpRecs[i].ID < tc.ExpRecs[j].ID
				})
				must.Eq(t, tc.ExpRecs, actualRecs)
			})
		}
	})
}

func TestHTTP_RecommendationGet(t *testing.T) {
	ci.Parallel(t)
	httpTest(t, nil, func(s *TestAgent) {

		// Create the job and recommendation
		wrongNs := mock.Namespace()
		ns := mock.Namespace()
		nsArgs := structs.NamespaceUpsertRequest{
			Namespaces: []*structs.Namespace{ns, wrongNs},
			WriteRequest: structs.WriteRequest{
				Region: "global",
			},
		}
		must.NoError(t, s.Agent.RPC("Namespace.UpsertNamespaces", &nsArgs, &structs.GenericResponse{}))
		job := mock.Job()
		job.Namespace = ns.Name
		jobArgs := structs.JobRegisterRequest{
			Job: job,
			WriteRequest: structs.WriteRequest{
				Region:    "global",
				Namespace: job.Namespace,
			},
		}
		must.NoError(t, s.Agent.RPC("Job.Register", &jobArgs, &structs.JobRegisterResponse{}))
		rec := mock.Recommendation(job)
		recUpsert := structs.RecommendationUpsertRequest{
			Recommendation: rec,
			WriteRequest: structs.WriteRequest{
				Region:    "global",
				Namespace: rec.Namespace,
			},
		}
		var recResp structs.SingleRecommendationResponse
		must.NoError(t, s.Agent.RPC("Recommendation.UpsertRecommendation", &recUpsert, &recResp))
		rec = recResp.Recommendation

		cases := []struct {
			Label  string
			URL    string
			ExpRec *structs.Recommendation
		}{
			{
				Label:  "extant rec",
				URL:    "/v1/recommendation/" + rec.ID,
				ExpRec: rec,
			},
			{
				Label:  "missing rec",
				URL:    "/v1/recommendation/" + uuid.Generate(),
				ExpRec: nil,
			},
			{
				Label:  "bad namespace",
				URL:    "/v1/recommendation/" + rec.ID + "?namespace=" + wrongNs.Name,
				ExpRec: rec,
			},
		}

		for _, tc := range cases {
			t.Run(tc.Label, func(t *testing.T) {
				req, err := http.NewRequest("GET", tc.URL, nil)
				must.NoError(t, err)
				respW := httptest.NewRecorder()

				obj, err := s.Server.RecommendationSpecificRequest(respW, req)

				must.NotEq(t, respW.Header().Get("X-Nomad-Index"), "", must.Sprint("missing index response header"))
				must.NotEq(t, respW.Header().Get("X-Nomad-KnownLeader"), "", must.Sprint("missing known leader response header"))
				must.NotEq(t, respW.Header().Get("X-Nomad-LastContact"), "", must.Sprint("missing last contact response header"))

				if tc.ExpRec == nil {
					must.Error(t, err)
					must.StrContains(t, err.Error(), "Recommendation not found")

				} else {
					must.NoError(t, err)
					must.NotNil(t, obj)
					out := obj.(*structs.Recommendation)
					must.Eq(t, tc.ExpRec, out)
				}
			})
		}
	})
}

func TestHTTP_RecommendationDelete(t *testing.T) {
	ci.Parallel(t)
	httpTest(t, nil, func(s *TestAgent) {

		// Create the job and recommendation
		wrongNs := mock.Namespace()
		ns := mock.Namespace()
		nsArgs := structs.NamespaceUpsertRequest{
			Namespaces: []*structs.Namespace{ns, wrongNs},
			WriteRequest: structs.WriteRequest{
				Region: "global",
			},
		}
		must.NoError(t, s.Agent.RPC("Namespace.UpsertNamespaces", &nsArgs, &structs.GenericResponse{}))
		job := mock.Job()
		job.Namespace = ns.Name
		jobArgs := structs.JobRegisterRequest{
			Job: job,
			WriteRequest: structs.WriteRequest{
				Region:    "global",
				Namespace: job.Namespace,
			},
		}
		must.NoError(t, s.Agent.RPC("Job.Register", &jobArgs, &structs.JobRegisterResponse{}))
		rec := mock.Recommendation(job)
		recUpsert := structs.RecommendationUpsertRequest{
			Recommendation: rec,
			WriteRequest: structs.WriteRequest{
				Region:    "global",
				Namespace: rec.Namespace,
			},
		}
		var recResp structs.SingleRecommendationResponse
		must.NoError(t, s.Agent.RPC("Recommendation.UpsertRecommendation", &recUpsert, &recResp))
		rec = recResp.Recommendation
		rec2 := mock.Recommendation(job)
		rec2.Resource = "MemoryMB"
		recUpsert.Recommendation = rec2
		must.NoError(t, s.Agent.RPC("Recommendation.UpsertRecommendation", &recUpsert, &recResp))
		rec2 = recResp.Recommendation

		cases := []struct {
			Label  string
			URL    string
			Error  string
			ExpRec *structs.Recommendation
		}{
			{
				Label:  "extant rec",
				URL:    "/v1/recommendation/" + rec.ID,
				ExpRec: rec,
			},
			{
				Label: "missing rec",
				URL:   "/v1/recommendation/" + uuid.Generate(),
				Error: "does not exist",
			},
			{
				Label:  "bad namespace",
				URL:    "/v1/recommendation/" + rec2.ID + "?namespace=" + wrongNs.Name,
				ExpRec: rec2,
			},
		}

		for _, tc := range cases {
			t.Run(tc.Label, func(t *testing.T) {
				req, err := http.NewRequest("DELETE", tc.URL, nil)
				must.NoError(t, err)
				respW := httptest.NewRecorder()

				obj, err := s.Server.RecommendationSpecificRequest(respW, req)
				if tc.Error != "" {
					must.Error(t, err)
					must.StrContains(t, err.Error(), tc.Error)
				} else {
					must.NoError(t, err)
					must.NotEq(t, respW.Header().Get("X-Nomad-Index"), "", must.Sprint("missing index response header"))

					out := obj.(*structs.GenericResponse)
					must.NonZero(t, out.Index)

					r, err := s.server.State().RecommendationByID(nil, rec.ID)
					must.NoError(t, err)
					must.Nil(t, r, must.Sprint("was not actually deleted"))
				}
			})
		}
	})
}

func TestHTTP_RecommendationCreate(t *testing.T) {
	ci.Parallel(t)
	httpTest(t, nil, func(s *TestAgent) {
		// Create the job and recommendation
		ns := mock.Namespace()
		nsArgs := structs.NamespaceUpsertRequest{
			Namespaces: []*structs.Namespace{ns},
			WriteRequest: structs.WriteRequest{
				Region: "global",
			},
		}
		must.NoError(t, s.Agent.RPC("Namespace.UpsertNamespaces", &nsArgs, &structs.GenericResponse{}))
		job := mock.Job()
		job.Namespace = ns.Name
		jobArgs := structs.JobRegisterRequest{
			Job: job,
			WriteRequest: structs.WriteRequest{
				Region:    "global",
				Namespace: job.Namespace,
			},
		}
		must.NoError(t, s.Agent.RPC("Job.Register", &jobArgs, &structs.JobRegisterResponse{}))

		rec := mock.Recommendation(job)
		buf := encodeReq(rec)
		req, err := http.NewRequest("PUT", "/v1/recommendation", buf)
		must.NoError(t, err)
		respW := httptest.NewRecorder()

		// Make the request
		obj, err := s.Server.RecommendationCreateRequest(respW, req)
		must.NoError(t, err)
		must.NotNil(t, obj)

		resp := obj.(*structs.Recommendation)

		// Check for the index
		must.NotEq(t, respW.Header().Get("X-Nomad-Index"), "")
		must.NotEq(t, resp.ID, "")
		must.NonZero(t, resp.CreateIndex)
		must.Eq(t, resp.CreateIndex, resp.ModifyIndex)

		must.Eq(t, &structs.Recommendation{
			ID:             resp.ID,
			Region:         "global",
			Namespace:      ns.Name,
			JobID:          job.ID,
			JobVersion:     0,
			Group:          rec.Group,
			Task:           rec.Task,
			Resource:       rec.Resource,
			Value:          rec.Value,
			Current:        job.TaskGroups[0].Tasks[0].Resources.CPU,
			Meta:           rec.Meta,
			Stats:          rec.Stats,
			EnforceVersion: false,
			SubmitTime:     resp.SubmitTime,
			CreateIndex:    resp.CreateIndex,
			ModifyIndex:    resp.ModifyIndex,
		}, resp)

		// Check that the recommendation was created
		existing, err := s.Agent.server.State().RecommendationByID(nil, resp.ID)
		must.NoError(t, err)
		must.NotNil(t, existing)
	})
}

func TestHTTP_RecommendationCreate_NoNils(t *testing.T) {
	ci.Parallel(t)
	httpTest(t, nil, func(s *TestAgent) {
		// Create the job and recommendation
		ns := mock.Namespace()
		nsArgs := structs.NamespaceUpsertRequest{
			Namespaces: []*structs.Namespace{ns},
			WriteRequest: structs.WriteRequest{
				Region: "global",
			},
		}
		must.NoError(t, s.Agent.RPC("Namespace.UpsertNamespaces", &nsArgs, &structs.GenericResponse{}))
		job := mock.Job()
		job.Namespace = ns.Name
		jobArgs := structs.JobRegisterRequest{
			Job: job,
			WriteRequest: structs.WriteRequest{
				Region:    "global",
				Namespace: job.Namespace,
			},
		}
		must.NoError(t, s.Agent.RPC("Job.Register", &jobArgs, &structs.JobRegisterResponse{}))

		rec := structsRecommendationToApi(mock.Recommendation(job))
		rec.ID = ""
		rec.Stats = nil
		rec.Meta = nil
		buf := encodeReq(rec)
		req, err := http.NewRequest("PUT", "/v1/recommendation", buf)
		must.NoError(t, err)
		respW := httptest.NewRecorder()

		// Make the request
		obj, err := s.Server.RecommendationCreateRequest(respW, req)
		must.NoError(t, err)
		must.NotNil(t, obj)

		resp := obj.(*structs.Recommendation)

		must.NotNil(t, resp.Meta)
		must.NotNil(t, resp.Stats)
		must.NotEq(t, resp.ID, "")

		existing, err := s.Agent.server.State().RecommendationByID(nil, resp.ID)
		must.NoError(t, err)
		must.NotNil(t, existing)
		must.Eq(t, map[string]interface{}{}, existing.Meta)
		must.Eq(t, map[string]float64{}, existing.Stats)
	})
}

func TestHTTP_RecommendationApply(t *testing.T) {
	ci.Parallel(t)
	httpTest(t, nil, func(s *TestAgent) {
		state := s.server.State()

		ns1 := mock.Namespace()
		ns2 := mock.Namespace()
		nsArgs := structs.NamespaceUpsertRequest{
			Namespaces: []*structs.Namespace{ns1, ns2},
			WriteRequest: structs.WriteRequest{
				Region: "global",
			},
		}
		must.NoError(t, s.Agent.RPC("Namespace.UpsertNamespaces", &nsArgs, &structs.GenericResponse{}))

		job1 := mock.Job()
		job1.Namespace = ns1.Name
		jobArgs := structs.JobRegisterRequest{
			Job: job1,
			WriteRequest: structs.WriteRequest{
				Region:    "global",
				Namespace: job1.Namespace,
			},
		}
		must.NoError(t, s.Agent.RPC("Job.Register", &jobArgs, &structs.JobRegisterResponse{}))
		rec1cpu := mock.Recommendation(job1)
		rec1cpu.Resource = "CPU"
		rec1cpu.Value = job1.TaskGroups[0].Tasks[0].Resources.CPU * 2
		recUpsert := structs.RecommendationUpsertRequest{
			Recommendation: rec1cpu,
			WriteRequest: structs.WriteRequest{
				Region:    "global",
				Namespace: rec1cpu.Namespace,
			},
		}
		var recResp structs.SingleRecommendationResponse
		must.NoError(t, s.Agent.RPC("Recommendation.UpsertRecommendation", &recUpsert, &recResp))
		rec1cpu = recResp.Recommendation
		rec1mem := mock.Recommendation(job1)
		rec1mem.Resource = "MemoryMB"
		rec1mem.Value = job1.TaskGroups[0].Tasks[0].Resources.CPU * 3
		recUpsert.Recommendation = rec1mem
		must.NoError(t, s.Agent.RPC("Recommendation.UpsertRecommendation", &recUpsert, &recResp))
		rec1mem = recResp.Recommendation

		job2 := mock.Job()
		job2.Namespace = ns1.Name
		jobArgs.Job = job2
		must.NoError(t, s.Agent.RPC("Job.Register", &jobArgs, &structs.JobRegisterResponse{}))
		rec2cpu := mock.Recommendation(job2)
		rec2cpu.Resource = "CPU"
		rec2cpu.Value = job2.TaskGroups[0].Tasks[0].Resources.CPU * 4
		recUpsert.Recommendation = rec2cpu
		must.NoError(t, s.Agent.RPC("Recommendation.UpsertRecommendation", &recUpsert, &recResp))
		rec2cpu = recResp.Recommendation
		rec2mem := mock.Recommendation(job2)
		rec2mem.Resource = "MemoryMB"
		rec2mem.Value = job2.TaskGroups[0].Tasks[0].Resources.MemoryMB * 5
		recUpsert.Recommendation = rec2mem
		must.NoError(t, s.Agent.RPC("Recommendation.UpsertRecommendation", &recUpsert, &recResp))
		rec2mem = recResp.Recommendation

		job3 := mock.Job()
		job3.Namespace = ns1.Name
		jobArgs.Job = job3
		must.NoError(t, s.Agent.RPC("Job.Register", &jobArgs, &structs.JobRegisterResponse{}))
		rec3cpu := mock.Recommendation(job3)
		rec3cpu.Resource = "CPU"
		rec3cpu.Value = job3.TaskGroups[0].Tasks[0].Resources.CPU * 6
		recUpsert.Recommendation = rec3cpu
		must.NoError(t, s.Agent.RPC("Recommendation.UpsertRecommendation", &recUpsert, &recResp))
		rec3cpu = recResp.Recommendation
		// rec3mem is actually invalid, we'll manually insert it into the state store to test errors
		rec3mem := mock.Recommendation(job3)
		rec3mem.Resource = "MemoryMB"
		rec3mem.Value = 5
		must.NoError(t, state.UpsertRecommendation(900, rec3mem))

		iter, _ := state.Recommendations(nil)
		for {
			raw := iter.Next()
			if raw == nil {
				break
			}
			fmt.Printf("%#v\n", raw.(*structs.Recommendation))
		}

		// Test 0: empty
		apply := api.RecommendationApplyRequest{}
		buf := encodeReq(apply)
		req, err := http.NewRequest("POST", "/v1/recommendations/apply", buf)
		must.NoError(t, err)
		respW := httptest.NewRecorder()
		obj, err := s.Server.RecommendationsApplyRequest(respW, req)
		must.Error(t, err)
		must.StrContains(t, err.Error(), "one or more recommendations")

		// Test 1: rec3mem should result in an error, rec1cpu should be fine, and delete rec1mem
		// only job1 CPU is updated
		apply = api.RecommendationApplyRequest{
			Apply:   []string{rec3mem.ID, rec1cpu.ID},
			Dismiss: []string{rec1mem.ID},
		}
		buf = encodeReq(apply)
		req, err = http.NewRequest("POST", "/v1/recommendations/apply", buf)
		must.NoError(t, err)
		respW = httptest.NewRecorder()
		obj, err = s.Server.RecommendationsApplyRequest(respW, req)
		must.NoError(t, err)
		must.NotEq(t, respW.Header().Get("X-Nomad-Index"), "", must.Sprint("missing index response header"))
		for _, id := range append(apply.Apply, apply.Dismiss...) {
			r, err := state.RecommendationByID(nil, id)
			must.NoError(t, err)
			must.Nil(t, r)
		}
		result := obj.(*api.RecommendationApplyResponse)
		must.Len(t, 1, result.Errors)
		must.Eq(t, result.Errors[0], &api.SingleRecommendationApplyError{
			Namespace:       job3.Namespace,
			JobID:           job3.ID,
			Recommendations: []string{rec3mem.ID},
			Error:           result.Errors[0].Error, // check this separately
		})
		must.StrContains(t, result.Errors[0].Error, "minimum MemoryMB value is 10")
		must.Len(t, 1, result.UpdatedJobs)
		job1, err = state.JobByID(nil, job1.Namespace, job1.ID)
		eval1, err := state.EvalByID(nil, result.UpdatedJobs[0].EvalID)
		must.NoError(t, err)
		must.NotNil(t, eval1)
		must.Eq(t, result.UpdatedJobs[0], &api.SingleRecommendationApplyResult{
			Namespace:       job1.Namespace,
			JobID:           job1.ID,
			JobModifyIndex:  job1.ModifyIndex,
			EvalID:          eval1.ID,
			EvalCreateIndex: eval1.CreateIndex,
			Warnings:        "",
			Recommendations: []string{rec1cpu.ID},
		})
		must.Eq(t, rec1cpu.Value, job1.TaskGroups[0].Tasks[0].Resources.CPU)

		// Test 2: rec3cpu, rec2cpu and rec2mem
		// test that job2 is updated with both, job3 with cpu
		apply = api.RecommendationApplyRequest{
			Apply: []string{rec3cpu.ID, rec2cpu.ID, rec2mem.ID},
		}
		buf = encodeReq(apply)
		req, err = http.NewRequest("POST", "/v1/recommendations/apply", buf)
		must.NoError(t, err)
		respW = httptest.NewRecorder()
		obj, err = s.Server.RecommendationsApplyRequest(respW, req)
		must.NoError(t, err)
		must.NotEq(t, respW.Header().Get("X-Nomad-Index"), "", must.Sprint("missing index response header"))
		for _, id := range append(apply.Apply, apply.Dismiss...) {
			r, err := state.RecommendationByID(nil, id)
			must.NoError(t, err)
			must.Nil(t, r)
		}
		//
		result = obj.(*api.RecommendationApplyResponse)
		must.Len(t, 0, result.Errors)
		must.Len(t, 2, result.UpdatedJobs)
		if result.UpdatedJobs[1].JobID == job2.ID {
			result.UpdatedJobs[0], result.UpdatedJobs[1] = result.UpdatedJobs[1], result.UpdatedJobs[0]
		}
		job2, err = state.JobByID(nil, job2.Namespace, job2.ID)
		must.NoError(t, err)
		job3, err = state.JobByID(nil, job3.Namespace, job3.ID)
		must.NoError(t, err)
		eval2, err := state.EvalByID(nil, result.UpdatedJobs[0].EvalID)
		must.NoError(t, err)
		must.NotNil(t, eval2)
		eval3, err := state.EvalByID(nil, result.UpdatedJobs[1].EvalID)
		must.NoError(t, err)
		must.NotNil(t, eval3)
		must.Eq(t, result.UpdatedJobs[0], &api.SingleRecommendationApplyResult{
			Namespace:       job2.Namespace,
			JobID:           job2.ID,
			JobModifyIndex:  job2.ModifyIndex,
			EvalID:          eval2.ID,
			EvalCreateIndex: eval2.CreateIndex,
			Warnings:        "",
			Recommendations: []string{rec2cpu.ID, rec2mem.ID},
		})
		must.Eq(t, rec2cpu.Value, job2.TaskGroups[0].Tasks[0].Resources.CPU)
		must.Eq(t, rec2mem.Value, job2.TaskGroups[0].Tasks[0].Resources.MemoryMB)
		must.Eq(t, result.UpdatedJobs[1], &api.SingleRecommendationApplyResult{
			Namespace:       job3.Namespace,
			JobID:           job3.ID,
			JobModifyIndex:  job3.ModifyIndex,
			EvalID:          eval3.ID,
			EvalCreateIndex: eval3.CreateIndex,
			Warnings:        "",
			Recommendations: []string{rec3cpu.ID},
		})
		must.Eq(t, rec3cpu.Value, job3.TaskGroups[0].Tasks[0].Resources.CPU)
		must.Eq(t, 256, job3.TaskGroups[0].Tasks[0].Resources.MemoryMB)
	})
}
