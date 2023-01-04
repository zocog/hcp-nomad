//go:build ent
// +build ent

package api

import (
	"testing"

	"github.com/hashicorp/nomad/api/internal/testutil"
	"github.com/shoenig/test/must"
)

func TestRecommendations_Upsert(t *testing.T) {
	testutil.Parallel(t)

	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Create a recommendation and register it
	ns := testNamespace()
	_, err := c.Namespaces().Register(ns, nil)
	must.NoError(t, err)
	job := testJob()
	job.Namespace = pointerOf(ns.Name)
	_, _, err = c.Jobs().Register(job, nil)
	must.NoError(t, err)
	rec := testRecommendation(job)
	rec, wm, err := recommendations.Upsert(rec, nil)
	must.NoError(t, err)
	assertWriteMeta(t, wm)

	// Query the recommendations back out again
	resp, qm, err := recommendations.List(&QueryOptions{
		Namespace: ns.Name,
	})
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.Len(t, 1, resp)
	must.Eq(t, rec.ID, resp[0].ID)
}

func TestRecommendations_Upsert_Invalid(t *testing.T) {
	testutil.Parallel(t)

	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Invalid resource
	ns := testNamespace()
	_, err := c.Namespaces().Register(ns, nil)
	must.NoError(t, err)
	job := testJob()
	job.Namespace = pointerOf(ns.Name)
	_, _, err = c.Jobs().Register(job, nil)
	must.NoError(t, err)
	rec := testRecommendation(job)
	rec.Resource = "Wrong"
	ret, _, err := recommendations.Upsert(rec, nil)
	must.Error(t, err)
	must.Nil(t, ret)
}

func TestRecommendation_Info(t *testing.T) {
	testutil.Parallel(t)

	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Create a recommendation and register it
	ns := testNamespace()
	_, err := c.Namespaces().Register(ns, nil)
	must.NoError(t, err)
	job := testJob()
	job.Namespace = pointerOf(ns.Name)
	_, _, err = c.Jobs().Register(job, nil)
	must.NoError(t, err)
	rec := testRecommendation(job)
	rec, wm, err := recommendations.Upsert(rec, nil)
	must.NoError(t, err)
	must.NotNil(t, rec)
	assertWriteMeta(t, wm)

	// Query the recommendation and compare
	info, qm, err := recommendations.Info(rec.ID, nil)
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.NotNil(t, info)
	must.Eq(t, rec, info)
}

func TestRecommendations_Delete(t *testing.T) {
	testutil.Parallel(t)

	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Create a recommendation and register it
	ns := testNamespace()
	_, err := c.Namespaces().Register(ns, nil)
	must.NoError(t, err)
	job := testJob()
	job.Namespace = pointerOf(ns.Name)
	_, _, err = c.Jobs().Register(job, nil)
	must.NoError(t, err)
	rec := testRecommendation(job)
	rec, wm, err := recommendations.Upsert(rec, nil)
	must.NoError(t, err)
	must.NotNil(t, rec)
	assertWriteMeta(t, wm)

	// Delete the recommendation
	wm, err = recommendations.Delete([]string{rec.ID}, nil)
	must.NoError(t, err)
	assertWriteMeta(t, wm)

	list, _, err := recommendations.List(&QueryOptions{
		Namespace: ns.Name,
	})
	must.NoError(t, err)
	must.Len(t, 0, list)
}

func TestRecommendations_List(t *testing.T) {
	testutil.Parallel(t)

	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Query all recommendations, should be empty
	resp, qm, err := recommendations.List(&QueryOptions{
		Namespace: "*",
	})
	must.NoError(t, err)
	must.Len(t, 0, resp)

	// Create recommendations in two namespaces
	ns1 := testNamespace()
	ns1.Name = generateUUID()
	_, err = c.Namespaces().Register(ns1, nil)
	must.NoError(t, err)
	job1 := testJob()
	job1.Namespace = pointerOf(ns1.Name)
	_, _, err = c.Jobs().Register(job1, nil)
	must.NoError(t, err)
	rec1 := testRecommendation(job1)
	rec1, wm, err := recommendations.Upsert(rec1, nil)
	must.NoError(t, err)
	assertWriteMeta(t, wm)

	ns2 := testNamespace()
	ns2.Name = generateUUID()
	_, err = c.Namespaces().Register(ns2, nil)
	must.NoError(t, err)
	job2 := testJob()
	job2.Namespace = pointerOf(ns2.Name)
	_, _, err = c.Jobs().Register(job2, nil)
	must.NoError(t, err)
	rec2 := testRecommendation(job2)
	rec2, wm, err = recommendations.Upsert(rec2, nil)
	must.NoError(t, err)
	assertWriteMeta(t, wm)

	// Query all recommendations, all namespaces
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: "*",
	})
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.Len(t, 2, resp)

	// Query the recommendations using a non-matching prefix
	prefix := generateUUID()
	for {
		if prefix[:2] != rec1.ID[:2] && prefix[:2] != rec2.ID[:2] {
			break
		}
		prefix = generateUUID()
	}
	resp, qm, err = recommendations.List(&QueryOptions{
		Prefix: prefix[0:2],
	})
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.Len(t, 0, resp)

	// Query the recommendations using a matching prefix
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns1.Name,
		Prefix:    rec1.ID[0:2],
	})
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.Len(t, 1, resp)
	must.Eq(t, rec1.ID, resp[0].ID)

	// Query the recommendations using a matching prefix, wrong namespace
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: "wrong",
		Prefix:    rec1.ID[0:2],
	})
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.Len(t, 0, resp)

	// Query the recommendations by namespace
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns1.Name,
	})
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.Len(t, 1, resp)
	must.Eq(t, resp[0].ID, rec1.ID)
}

func TestRecommendations_ListFilter(t *testing.T) {
	testutil.Parallel(t)

	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Query all recommendations, should be empty
	resp, qm, err := recommendations.List(&QueryOptions{
		Namespace: "*",
	})
	must.Nil(t, err)
	must.Len(t, 0, resp)

	// Create recommendations in two namespaces
	ns := testNamespace()
	ns.Name = generateUUID()
	_, err = c.Namespaces().Register(ns, nil)
	must.NoError(t, err)
	job1 := testJob()
	job1.Namespace = pointerOf(ns.Name)
	_, _, err = c.Jobs().Register(job1, nil)
	must.NoError(t, err)
	rec1 := testRecommendation(job1)
	rec1, _, err = recommendations.Upsert(rec1, nil)
	must.NoError(t, err)
	job2 := testJob()
	job2.ID = pointerOf(generateUUID())
	job2.Namespace = pointerOf(ns.Name)
	_, _, err = c.Jobs().Register(job2, nil)
	must.NoError(t, err)
	rec2 := testRecommendation(job2)
	_, _, err = recommendations.Upsert(rec2, nil)
	must.NoError(t, err)

	// By namespace
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params:    map[string]string{},
	})
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.Len(t, 2, resp)

	// By job, present
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params: map[string]string{
			"job": *job1.ID,
		},
	})
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.Len(t, 1, resp)
	must.Eq(t, rec1.ID, resp[0].ID)

	// By job, absent
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params: map[string]string{
			"job": "wrong",
		},
	})
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.Len(t, 0, resp)

	// Task group
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params: map[string]string{
			"job":   *job1.ID,
			"group": *job1.TaskGroups[0].Name,
		},
	})
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.Len(t, 1, resp)
	must.Eq(t, rec1.ID, resp[0].ID)

	// Task group, absent
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params: map[string]string{
			"job":   *job1.ID,
			"group": "wrong",
		},
	})
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.Len(t, 0, resp)

	// Task
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params: map[string]string{
			"job":   *job1.ID,
			"group": *job1.TaskGroups[0].Name,
			"task":  job1.TaskGroups[0].Tasks[0].Name,
		},
	})
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.Len(t, 1, resp)
	must.Eq(t, rec1.ID, resp[0].ID)

	// Task, absent
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params: map[string]string{
			"job":   *job1.ID,
			"group": *job1.TaskGroups[0].Name,
			"task":  "wrong",
		},
	})
	must.Nil(t, err)
	assertQueryMeta(t, qm)
	must.Len(t, 0, resp)
}

func TestRecommendations_Apply(t *testing.T) {
	testutil.Parallel(t)

	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Query all recommendations, should be empty
	resp, _, err := recommendations.List(&QueryOptions{
		Namespace: "*",
	})
	must.NoError(t, err)
	must.Len(t, 0, resp)

	// Create recommendations in two namespaces
	ns := testNamespace()
	ns.Name = generateUUID()
	_, err = c.Namespaces().Register(ns, nil)
	must.NoError(t, err)
	job1 := testJob()
	job1.Namespace = pointerOf(ns.Name)
	_, _, err = c.Jobs().Register(job1, nil)
	must.NoError(t, err)
	rec1cpu := testRecommendation(job1)
	rec1cpu, _, err = recommendations.Upsert(rec1cpu, nil)
	must.NoError(t, err)
	rec1mem := testRecommendation(job1)
	rec1mem.Resource = "MemoryMB"
	rec1mem.Value = *job1.TaskGroups[0].Tasks[0].Resources.MemoryMB * 2
	rec1mem, _, err = recommendations.Upsert(rec1mem, nil)
	must.NoError(t, err)
	job2 := testJob()
	job2.ID = pointerOf(generateUUID())
	job2.Namespace = pointerOf(ns.Name)
	_, _, err = c.Jobs().Register(job2, nil)
	must.NoError(t, err)
	rec2cpu := testRecommendation(job2)
	rec2cpu, _, err = recommendations.Upsert(rec2cpu, nil)
	must.NoError(t, err)
	rec2mem := testRecommendation(job2)
	rec2mem.Resource = "MemoryMB"
	rec2mem.Value = *job2.TaskGroups[0].Tasks[0].Resources.MemoryMB * 2
	rec2mem, _, err = recommendations.Upsert(rec2mem, nil)
	must.NoError(t, err)

	// Recs are present
	resp, _, err = recommendations.List(&QueryOptions{
		Namespace: "*",
	})
	must.NoError(t, err)
	must.Len(t, 4, resp)

	// Apply recs
	applyResponse, wm, err := recommendations.Apply(
		[]string{rec1cpu.ID, rec2cpu.ID, rec2mem.ID},
		false)
	must.NoError(t, err)
	must.Len(t, 0, applyResponse.Errors)
	must.Len(t, 2, applyResponse.UpdatedJobs)
	assertWriteMeta(t, &applyResponse.WriteMeta)
	assertWriteMeta(t, wm)

	// Jobs are updated
	job1, _, _ = c.Jobs().Info(*job1.ID, &QueryOptions{Namespace: *job1.Namespace})
	must.Greater(t, *job1.CreateIndex, *job1.JobModifyIndex)
	must.Eq(t, rec1cpu.Value, *job1.TaskGroups[0].Tasks[0].Resources.CPU)
	must.Eq(t, 256, *job1.TaskGroups[0].Tasks[0].Resources.MemoryMB)
	job2, _, _ = c.Jobs().Info(*job2.ID, &QueryOptions{Namespace: *job2.Namespace})
	must.Greater(t, *job2.CreateIndex, *job2.JobModifyIndex)
	must.Eq(t, rec2cpu.Value, *job2.TaskGroups[0].Tasks[0].Resources.CPU)
	must.Eq(t, rec2mem.Value, *job2.TaskGroups[0].Tasks[0].Resources.MemoryMB)

	// Applied recs are gone
	resp, _, err = recommendations.List(&QueryOptions{
		Namespace: "*",
	})
	must.NoError(t, err)
	must.Len(t, 1, resp)
	must.Eq(t, rec1mem.ID, resp[0].ID)
}
