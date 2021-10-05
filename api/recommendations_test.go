//go:build ent
// +build ent

package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecommendations_Upsert(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Create a recommendation and register it
	ns := testNamespace()
	_, err := c.Namespaces().Register(ns, nil)
	require.NoError(err)
	job := testJob()
	job.Namespace = stringToPtr(ns.Name)
	_, _, err = c.Jobs().Register(job, nil)
	require.NoError(err)
	rec := testRecommendation(job)
	rec, wm, err := recommendations.Upsert(rec, nil)
	require.NoError(err)
	assertWriteMeta(t, wm)

	// Query the recommendations back out again
	resp, qm, err := recommendations.List(&QueryOptions{
		Namespace: ns.Name,
	})
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.Len(resp, 1)
	require.Equal(rec.ID, resp[0].ID)
}

func TestRecommendations_Upsert_Invalid(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Invalid resource
	ns := testNamespace()
	_, err := c.Namespaces().Register(ns, nil)
	require.NoError(err)
	job := testJob()
	job.Namespace = stringToPtr(ns.Name)
	_, _, err = c.Jobs().Register(job, nil)
	require.NoError(err)
	rec := testRecommendation(job)
	rec.Resource = "Wrong"
	ret, _, err := recommendations.Upsert(rec, nil)
	require.Error(err)
	require.Nil(ret)
}

func TestRecommendation_Info(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Create a recommendation and register it
	ns := testNamespace()
	_, err := c.Namespaces().Register(ns, nil)
	require.NoError(err)
	job := testJob()
	job.Namespace = stringToPtr(ns.Name)
	_, _, err = c.Jobs().Register(job, nil)
	require.NoError(err)
	rec := testRecommendation(job)
	rec, wm, err := recommendations.Upsert(rec, nil)
	require.NoError(err)
	require.NotNil(rec)
	assertWriteMeta(t, wm)

	// Query the recommendation and compare
	info, qm, err := recommendations.Info(rec.ID, nil)
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.NotNil(info)
	require.Equal(rec, info)
}

func TestRecommendations_Delete(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Create a recommendation and register it
	ns := testNamespace()
	_, err := c.Namespaces().Register(ns, nil)
	require.NoError(err)
	job := testJob()
	job.Namespace = stringToPtr(ns.Name)
	_, _, err = c.Jobs().Register(job, nil)
	require.NoError(err)
	rec := testRecommendation(job)
	rec, wm, err := recommendations.Upsert(rec, nil)
	require.NoError(err)
	require.NotNil(rec)
	assertWriteMeta(t, wm)

	// Delete the recommendation
	wm, err = recommendations.Delete([]string{rec.ID}, nil)
	require.NoError(err)
	assertWriteMeta(t, wm)

	list, _, err := recommendations.List(&QueryOptions{
		Namespace: ns.Name,
	})
	require.NoError(err)
	require.Len(list, 0)
}

func TestRecommendations_List(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Query all recommendations, should be empty
	resp, qm, err := recommendations.List(&QueryOptions{
		Namespace: "*",
	})
	require.Nil(err)
	require.Empty(resp)

	// Create recommendations in two namespaces
	ns1 := testNamespace()
	ns1.Name = generateUUID()
	_, err = c.Namespaces().Register(ns1, nil)
	require.NoError(err)
	job1 := testJob()
	job1.Namespace = stringToPtr(ns1.Name)
	_, _, err = c.Jobs().Register(job1, nil)
	require.NoError(err)
	rec1 := testRecommendation(job1)
	rec1, wm, err := recommendations.Upsert(rec1, nil)
	require.NoError(err)
	assertWriteMeta(t, wm)

	ns2 := testNamespace()
	ns2.Name = generateUUID()
	_, err = c.Namespaces().Register(ns2, nil)
	require.NoError(err)
	job2 := testJob()
	job2.Namespace = stringToPtr(ns2.Name)
	_, _, err = c.Jobs().Register(job2, nil)
	require.NoError(err)
	rec2 := testRecommendation(job2)
	rec2, wm, err = recommendations.Upsert(rec2, nil)
	require.NoError(err)
	assertWriteMeta(t, wm)

	// Query all recommendations, all namespaces
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: "*",
	})
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.Len(resp, 2)

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
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.Len(resp, 0)

	// Query the recommendations using a matching prefix
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns1.Name,
		Prefix:    rec1.ID[0:2],
	})
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.Len(resp, 1)
	require.Equal(rec1.ID, resp[0].ID)

	// Query the recommendations using a matching prefix, wrong namespace
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: "wrong",
		Prefix:    rec1.ID[0:2],
	})
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.Len(resp, 0)

	// Query the recommendations by namespace
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns1.Name,
	})
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.Len(resp, 1)
	require.Equal(resp[0].ID, rec1.ID)
}

func TestRecommendations_ListFilter(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Query all recommendations, should be empty
	resp, qm, err := recommendations.List(&QueryOptions{
		Namespace: "*",
	})
	require.Nil(err)
	require.Empty(resp)

	// Create recommendations in two namespaces
	ns := testNamespace()
	ns.Name = generateUUID()
	_, err = c.Namespaces().Register(ns, nil)
	require.NoError(err)
	job1 := testJob()
	job1.Namespace = stringToPtr(ns.Name)
	_, _, err = c.Jobs().Register(job1, nil)
	require.NoError(err)
	rec1 := testRecommendation(job1)
	rec1, _, err = recommendations.Upsert(rec1, nil)
	require.NoError(err)
	job2 := testJob()
	job2.ID = stringToPtr(generateUUID())
	job2.Namespace = stringToPtr(ns.Name)
	_, _, err = c.Jobs().Register(job2, nil)
	require.NoError(err)
	rec2 := testRecommendation(job2)
	_, _, err = recommendations.Upsert(rec2, nil)
	require.NoError(err)

	// By namespace
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params:    map[string]string{},
	})
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.Len(resp, 2)

	// By job, present
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params: map[string]string{
			"job": *job1.ID,
		},
	})
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.Len(resp, 1)
	require.Equal(rec1.ID, resp[0].ID)

	// By job, absent
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params: map[string]string{
			"job": "wrong",
		},
	})
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.Len(resp, 0)

	// Task group
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params: map[string]string{
			"job":   *job1.ID,
			"group": *job1.TaskGroups[0].Name,
		},
	})
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.Len(resp, 1)
	require.Equal(rec1.ID, resp[0].ID)

	// Task group, absent
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params: map[string]string{
			"job":   *job1.ID,
			"group": "wrong",
		},
	})
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.Len(resp, 0)

	// Task
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params: map[string]string{
			"job":   *job1.ID,
			"group": *job1.TaskGroups[0].Name,
			"task":  job1.TaskGroups[0].Tasks[0].Name,
		},
	})
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.Len(resp, 1)
	require.Equal(rec1.ID, resp[0].ID)

	// Task, absent
	resp, qm, err = recommendations.List(&QueryOptions{
		Namespace: ns.Name,
		Params: map[string]string{
			"job":   *job1.ID,
			"group": *job1.TaskGroups[0].Name,
			"task":  "wrong",
		},
	})
	require.Nil(err)
	assertQueryMeta(t, qm)
	require.Len(resp, 0)
}

func TestRecommendations_Apply(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	c, s := makeClient(t, nil, nil)
	defer s.Stop()
	recommendations := c.Recommendations()

	// Query all recommendations, should be empty
	resp, _, err := recommendations.List(&QueryOptions{
		Namespace: "*",
	})
	require.Nil(err)
	require.Empty(resp)

	// Create recommendations in two namespaces
	ns := testNamespace()
	ns.Name = generateUUID()
	_, err = c.Namespaces().Register(ns, nil)
	require.NoError(err)
	job1 := testJob()
	job1.Namespace = stringToPtr(ns.Name)
	_, _, err = c.Jobs().Register(job1, nil)
	require.NoError(err)
	rec1cpu := testRecommendation(job1)
	rec1cpu, _, err = recommendations.Upsert(rec1cpu, nil)
	require.NoError(err)
	rec1mem := testRecommendation(job1)
	rec1mem.Resource = "MemoryMB"
	rec1mem.Value = *job1.TaskGroups[0].Tasks[0].Resources.MemoryMB * 2
	rec1mem, _, err = recommendations.Upsert(rec1mem, nil)
	require.NoError(err)
	job2 := testJob()
	job2.ID = stringToPtr(generateUUID())
	job2.Namespace = stringToPtr(ns.Name)
	_, _, err = c.Jobs().Register(job2, nil)
	require.NoError(err)
	rec2cpu := testRecommendation(job2)
	rec2cpu, _, err = recommendations.Upsert(rec2cpu, nil)
	require.NoError(err)
	rec2mem := testRecommendation(job2)
	rec2mem.Resource = "MemoryMB"
	rec2mem.Value = *job2.TaskGroups[0].Tasks[0].Resources.MemoryMB * 2
	rec2mem, _, err = recommendations.Upsert(rec2mem, nil)
	require.NoError(err)

	// Recs are present
	resp, _, err = recommendations.List(&QueryOptions{
		Namespace: "*",
	})
	require.Nil(err)
	require.Len(resp, 4)

	// Apply recs
	applyResponse, wm, err := recommendations.Apply(
		[]string{rec1cpu.ID, rec2cpu.ID, rec2mem.ID},
		false)
	require.NoError(err)
	require.Empty(applyResponse.Errors)
	require.Len(applyResponse.UpdatedJobs, 2)
	assertWriteMeta(t, &applyResponse.WriteMeta)
	assertWriteMeta(t, wm)

	// Jobs are updated
	job1, _, _ = c.Jobs().Info(*job1.ID, &QueryOptions{Namespace: *job1.Namespace})
	require.Greater(*job1.JobModifyIndex, *job1.CreateIndex)
	require.Equal(rec1cpu.Value, *job1.TaskGroups[0].Tasks[0].Resources.CPU)
	require.Equal(256, *job1.TaskGroups[0].Tasks[0].Resources.MemoryMB)
	job2, _, _ = c.Jobs().Info(*job2.ID, &QueryOptions{Namespace: *job2.Namespace})
	require.Greater(*job2.JobModifyIndex, *job2.CreateIndex)
	require.Equal(rec2cpu.Value, *job2.TaskGroups[0].Tasks[0].Resources.CPU)
	require.Equal(rec2mem.Value, *job2.TaskGroups[0].Tasks[0].Resources.MemoryMB)

	// Applied recs are gone
	resp, _, err = recommendations.List(&QueryOptions{
		Namespace: "*",
	})
	require.Nil(err)
	require.Len(resp, 1)
	require.Equal(rec1mem.ID, resp[0].ID)
}
