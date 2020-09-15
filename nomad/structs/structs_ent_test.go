// +build ent

package structs

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/go-msgpack/codec"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/nomad/helper/uuid"
)

func TestNamespace_Validate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		Test      string
		Namespace *Namespace
		Expected  string
	}{
		{
			Test: "empty name",
			Namespace: &Namespace{
				Name: "",
			},
			Expected: "invalid name",
		},
		{
			Test: "slashes in name",
			Namespace: &Namespace{
				Name: "foo/bar",
			},
			Expected: "invalid name",
		},
		{
			Test: "too long name",
			Namespace: &Namespace{
				Name: strings.Repeat("a", 200),
			},
			Expected: "invalid name",
		},
		{
			Test: "too long description",
			Namespace: &Namespace{
				Name:        "foo",
				Description: strings.Repeat("a", 300),
			},
			Expected: "description longer than",
		},
		{
			Test: "valid",
			Namespace: &Namespace{
				Name:        "foo",
				Description: "bar",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.Test, func(t *testing.T) {
			err := c.Namespace.Validate()
			if err == nil {
				if c.Expected == "" {
					return
				}

				t.Fatalf("Expected error %q; got nil", c.Expected)
			} else if c.Expected == "" {
				t.Fatalf("Unexpected error %v", err)
			} else if !strings.Contains(err.Error(), c.Expected) {
				t.Fatalf("Expected error %q; got %v", c.Expected, err)
			}
		})
	}
}

func TestNamespace_SetHash(t *testing.T) {
	assert := assert.New(t)
	ns := &Namespace{
		Name:        "foo",
		Description: "bar",
	}
	out1 := ns.SetHash()
	assert.NotNil(out1)
	assert.NotNil(ns.Hash)
	assert.Equal(out1, ns.Hash)

	ns.Description = "bam"
	out2 := ns.SetHash()
	assert.NotNil(out2)
	assert.NotNil(ns.Hash)
	assert.Equal(out2, ns.Hash)
	assert.NotEqual(out1, out2)
}

func TestSentinelPolicySetHash(t *testing.T) {
	sp := &SentinelPolicy{
		Name:             "test",
		Description:      "Great policy",
		Scope:            SentinelScopeSubmitJob,
		EnforcementLevel: SentinelEnforcementLevelAdvisory,
		Policy:           "main = rule { true }",
	}

	out1 := sp.SetHash()
	assert.NotNil(t, out1)
	assert.NotNil(t, sp.Hash)
	assert.Equal(t, out1, sp.Hash)

	sp.Policy = "main = rule { false }"
	out2 := sp.SetHash()
	assert.NotNil(t, out2)
	assert.NotNil(t, sp.Hash)
	assert.Equal(t, out2, sp.Hash)
	assert.NotEqual(t, out1, out2)
}

func TestSentinelPolicy_Validate(t *testing.T) {
	sp := &SentinelPolicy{
		Name:             "test",
		Description:      "Great policy",
		Scope:            SentinelScopeSubmitJob,
		EnforcementLevel: SentinelEnforcementLevelAdvisory,
		Policy:           "main = rule { true }",
	}

	// Test a good policy
	assert.Nil(t, sp.Validate())

	// Try an invalid name
	sp.Name = "hi@there"
	assert.NotNil(t, sp.Validate())

	// Try an invalid description
	sp.Name = "test"
	sp.Description = string(make([]byte, 1000))
	assert.NotNil(t, sp.Validate())

	// Try an invalid scope
	sp.Description = ""
	sp.Scope = "random"
	assert.NotNil(t, sp.Validate())

	// Try an invalid type
	sp.Scope = SentinelScopeSubmitJob
	sp.EnforcementLevel = "yolo"
	assert.NotNil(t, sp.Validate())

	// Try an invalid policy
	sp.EnforcementLevel = SentinelEnforcementLevelAdvisory
	sp.Policy = "blah 123"
	assert.NotNil(t, sp.Validate())
}

func TestSentinelPolicy_CacheKey(t *testing.T) {
	sp := &SentinelPolicy{
		Name:        "test",
		ModifyIndex: 10,
	}
	assert.Equal(t, "test:10", sp.CacheKey())
}

func TestSentinelPolicy_Compile(t *testing.T) {
	sp := &SentinelPolicy{
		Name:             "test",
		Description:      "Great policy",
		Scope:            SentinelScopeSubmitJob,
		EnforcementLevel: SentinelEnforcementLevelAdvisory,
		Policy:           "main = rule { true }",
	}

	f, fset, err := sp.Compile()
	assert.Nil(t, err)
	assert.NotNil(t, fset)
	assert.NotNil(t, f)
}

func TestQuotaSpec_Validate(t *testing.T) {
	cases := []struct {
		Name   string
		Spec   *QuotaSpec
		Errors []string
	}{
		{
			Name: "valid",
			Spec: &QuotaSpec{
				Name:        "foo",
				Description: "limit foo",
				Limits: []*QuotaLimit{
					{
						Region: "global",
						RegionLimit: &Resources{
							CPU:      5000,
							MemoryMB: 2000,
						},
					},
				},
			},
		},
		{
			Name: "bad name, description, missing quota",
			Spec: &QuotaSpec{
				Name:        "*",
				Description: strings.Repeat("a", 1000),
			},
			Errors: []string{
				"invalid name",
				"description longer",
				"must provide at least one quota limit",
			},
		},
		{
			Name: "bad limit",
			Spec: &QuotaSpec{
				Name: "bad-limit",
				Limits: []*QuotaLimit{
					{},
				},
			},
			Errors: []string{
				"must provide a region",
				"must provide a region limit",
			},
		},
		{
			Name: "bad limit resources",
			Spec: &QuotaSpec{
				Name: "bad-limit-resources",
				Limits: []*QuotaLimit{
					{
						Region: "foo",
						RegionLimit: &Resources{
							DiskMB: 500,
							Networks: []*NetworkResource{
								{},
							},
						},
					},
				},
			},
			Errors: []string{
				"limit disk",
			},
		},
		{
			Name: "valid with network",
			Spec: &QuotaSpec{
				Name: "valid-with-network",
				Limits: []*QuotaLimit{
					{
						Region: "foo",
						RegionLimit: &Resources{
							Networks: []*NetworkResource{
								{MBits: 45},
							},
						},
					},
				},
			},
		},
		{
			Name: "invalid network cidr",
			Spec: &QuotaSpec{
				Name: "invalid-network-cidr",
				Limits: []*QuotaLimit{
					{
						Region: "foo",
						RegionLimit: &Resources{
							Networks: []*NetworkResource{
								{MBits: 45, CIDR: "0.0.0.255"},
							},
						},
					},
				},
			},
			Errors: []string{
				"only network mbits may be limited",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			err := c.Spec.Validate()
			if err == nil {
				if len(c.Errors) != 0 {
					t.Fatalf("expected errors: %v", c.Errors)
				}
			} else {
				if len(c.Errors) == 0 {
					t.Fatalf("unexpected error: %v", err)
				} else {
					for _, exp := range c.Errors {
						if !strings.Contains(err.Error(), exp) {
							t.Fatalf("expected error to contain %q; got %v", exp, err)
						}
					}
				}
			}
		})
	}
}

func TestQuotaSpec_SetHash(t *testing.T) {
	assert := assert.New(t)
	qs := &QuotaSpec{
		Name:        "test",
		Description: "test limits",
		Limits: []*QuotaLimit{
			{
				Region: "foo",
				RegionLimit: &Resources{
					CPU: 5000,
				},
			},
		},
	}

	out1 := qs.SetHash()
	assert.NotNil(out1)
	assert.NotNil(qs.Hash)
	assert.Equal(out1, qs.Hash)

	qs.Name = "foo"
	out2 := qs.SetHash()
	assert.NotNil(out2)
	assert.NotNil(qs.Hash)
	assert.Equal(out2, qs.Hash)
	assert.NotEqual(out1, out2)
}

// Test that changing a region limit will also stimulate a hash change
func TestQuotaSpec_SetHash2(t *testing.T) {
	assert := assert.New(t)
	qs := &QuotaSpec{
		Name:        "test",
		Description: "test limits",
		Limits: []*QuotaLimit{
			{
				Region: "foo",
				RegionLimit: &Resources{
					CPU: 5000,
				},
			},
		},
	}

	out1 := qs.SetHash()
	assert.NotNil(out1)
	assert.NotNil(qs.Hash)
	assert.Equal(out1, qs.Hash)

	qs.Limits[0].RegionLimit.CPU = 2000
	out2 := qs.SetHash()
	assert.NotNil(out2)
	assert.NotNil(qs.Hash)
	assert.Equal(out2, qs.Hash)
	assert.NotEqual(out1, out2)
}

func TestQuotaUsage_Diff(t *testing.T) {
	cases := []struct {
		Name   string
		Usage  *QuotaUsage
		Spec   *QuotaSpec
		Create []string
		Delete []string
	}{
		{
			Name:   "noop",
			Create: []string{},
			Delete: []string{},
		},
		{
			Name: "no usage",
			Spec: &QuotaSpec{
				Name:        "foo",
				Description: "limit foo",
				Limits: []*QuotaLimit{
					{
						Region: "global",
						RegionLimit: &Resources{
							CPU:      5000,
							MemoryMB: 2000,
						},
						Hash: []byte{0x1},
					},
					{
						Region: "foo",
						RegionLimit: &Resources{
							CPU:      5000,
							MemoryMB: 2000,
						},
						Hash: []byte{0x2},
					},
				},
			},
			Create: []string{string(rune(0x1)), string(rune(0x2))},
			Delete: []string{},
		},
		{
			Name: "no spec",
			Usage: &QuotaUsage{
				Name: "foo",
				Used: map[string]*QuotaLimit{
					"\x01": {
						Region: "global",
						RegionLimit: &Resources{
							CPU:      5000,
							MemoryMB: 2000,
						},
						Hash: []byte{0x1},
					},
					"\x02": {
						Region: "foo",
						RegionLimit: &Resources{
							CPU:      5000,
							MemoryMB: 2000,
						},
						Hash: []byte{0x2},
					},
				},
			},
			Create: []string{},
			Delete: []string{string(rune(0x1)), string(rune(0x2))},
		},
		{
			Name: "both",
			Spec: &QuotaSpec{
				Name:        "foo",
				Description: "limit foo",
				Limits: []*QuotaLimit{
					{
						Region: "global",
						RegionLimit: &Resources{
							CPU:      5000,
							MemoryMB: 2000,
						},
						Hash: []byte{0x1},
					},
					{
						Region: "foo",
						RegionLimit: &Resources{
							CPU:      5000,
							MemoryMB: 2000,
						},
						Hash: []byte{0x2},
					},
				},
			},
			Usage: &QuotaUsage{
				Name: "foo",
				Used: map[string]*QuotaLimit{
					"\x01": {
						Region: "global",
						RegionLimit: &Resources{
							CPU:      5000,
							MemoryMB: 2000,
						},
						Hash: []byte{0x1},
					},
					"\x03": {
						Region: "bar",
						RegionLimit: &Resources{
							CPU:      5000,
							MemoryMB: 2000,
						},
						Hash: []byte{0x3},
					},
				},
			},
			Create: []string{string(rune(0x2))},
			Delete: []string{string(rune(0x3))},
		},
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			actCreate, actDelete := c.Usage.DiffLimits(c.Spec)
			actCreateHashes := make([]string, 0, len(actCreate))
			actDeleteHashes := make([]string, 0, len(actDelete))
			for _, c := range actCreate {
				actCreateHashes = append(actCreateHashes, string(c.Hash))
			}
			for _, d := range actDelete {
				actDeleteHashes = append(actDeleteHashes, string(d.Hash))
			}

			sort.Strings(actCreateHashes)
			sort.Strings(actDeleteHashes)
			sort.Strings(c.Create)
			sort.Strings(c.Delete)
			assert.Equal(t, actCreateHashes, c.Create)
			assert.Equal(t, actDeleteHashes, c.Delete)
		})
	}
}

func TestQuotaLimit_Superset(t *testing.T) {
	l1 := &QuotaLimit{
		Region: "foo",
		RegionLimit: &Resources{
			CPU:      1000,
			MemoryMB: 1000,
		},
	}
	l2 := l1.Copy()
	l3 := l1.Copy()
	l3.RegionLimit.CPU++
	l3.RegionLimit.MemoryMB++

	superset, _ := l1.Superset(l2)
	assert.True(t, superset)

	superset, dimensions := l1.Superset(l3)
	assert.False(t, superset)
	assert.Len(t, dimensions, 2)

	l4 := l1.Copy()
	l4.RegionLimit.MemoryMB = 0
	l4.RegionLimit.CPU = 0
	superset, _ = l4.Superset(l3)
	assert.True(t, superset)

	l5 := l1.Copy()
	l5.RegionLimit.MemoryMB = -1
	l5.RegionLimit.CPU = -1
	superset, dimensions = l5.Superset(l3)
	assert.False(t, superset)
	assert.Len(t, dimensions, 2)
}

// TestQuotaUsageSerialization tests that custom json
// marshalling functions get exercised to base64 QuotaUsage.Used
// map keys, which are binary bytes
func TestQuotaUsageSerialization(t *testing.T) {
	input := QuotaUsage{
		Name: "foo",
		Used: map[string]*QuotaLimit{
			"\x01": {
				Region: "global",
				RegionLimit: &Resources{
					CPU:      5000,
					MemoryMB: 2000,
				},
				Hash: []byte{0x1},
			},
		},
	}

	var buf bytes.Buffer
	encoder := codec.NewEncoder(&buf, JsonHandle)
	require.NoError(t, encoder.Encode(input))

	// ensure that Used key is a base64("\x01"") == `AQ==`
	require.Contains(t, buf.String(), `"Used":{"AQ==":{`)

	var out QuotaUsage
	decoder := codec.NewDecoder(&buf, JsonHandle)
	require.NoError(t, decoder.Decode(&out))

	require.Equal(t, input, out)
}

func TestMultiregion_Validate(t *testing.T) {
	require := require.New(t)
	cases := []struct {
		Name    string
		JobType string
		Case    *Multiregion
		Errors  []string
	}{
		{
			Name:    "empty valid multiregion spec",
			JobType: JobTypeService,
			Case:    &Multiregion{},
			Errors:  []string{},
		},

		{
			Name:    "non-empty valid multiregion spec",
			JobType: JobTypeService,
			Case: &Multiregion{
				Strategy: &MultiregionStrategy{
					MaxParallel: 2,
					OnFailure:   "fail_all",
				},
				Regions: []*MultiregionRegion{
					{

						Count:       2,
						Datacenters: []string{"west-1", "west-2"},
						Meta:        map[string]string{},
					},
					{
						Name:        "east",
						Count:       1,
						Datacenters: []string{"east-1"},
						Meta:        map[string]string{},
					},
				},
			},
			Errors: []string{},
		},

		{
			Name:    "repeated region, wrong strategy, missing DCs",
			JobType: JobTypeBatch,
			Case: &Multiregion{
				Strategy: &MultiregionStrategy{
					MaxParallel: 2,
				},
				Regions: []*MultiregionRegion{
					{
						Name:        "west",
						Datacenters: []string{"west-1", "west-2"},
					},

					{
						Name: "west",
					},
				},
			},
			Errors: []string{
				"Multiregion region \"west\" can't be listed twice",
				"Multiregion region \"west\" must have at least 1 datacenter",
				"Multiregion batch jobs can't have an update strategy",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			err := tc.Case.Validate(tc.JobType, []string{})
			if len(tc.Errors) == 0 {
				require.NoError(err)
			} else {
				mErr := err.(*multierror.Error)
				for i, expectedErr := range tc.Errors {
					if !strings.Contains(mErr.Errors[i].Error(), expectedErr) {
						t.Fatalf("err: %s, expected: %s", err, expectedErr)
					}
				}
			}
		})
	}
}

func TestRecommendation_Validate(t *testing.T) {
	cases := []struct {
		Name     string
		Rec      *Recommendation
		Error    bool
		ErrorMsg string
	}{
		{
			Name: "missing region",
			Rec: &Recommendation{
				Value:     -1,
				Group:     "web",
				Task:      "nginx",
				Resource:  "CPU",
				Namespace: "default",
				JobID:     "example",
			},
			Error:    true,
			ErrorMsg: "region",
		},
		{
			Name: "requires group",
			Rec: &Recommendation{
				Value:     10,
				Task:      "nginx",
				Resource:  "CPU",
				Region:    "global",
				Namespace: "default",
				JobID:     "example",
			},
			Error:    true,
			ErrorMsg: "must contain a group",
		},
		{
			Name: "requires task",
			Rec: &Recommendation{
				Value:     10,
				Group:     "web",
				Resource:  "CPU",
				Region:    "global",
				Namespace: "default",
				JobID:     "example",
			},
			Error:    true,
			ErrorMsg: "must contain a task",
		},
		{
			Name: "requires resource",
			Rec: &Recommendation{
				Value:     10,
				Group:     "web",
				Task:      "nginx",
				Region:    "global",
				Namespace: "default",
				JobID:     "example",
			},
			Error:    true,
			ErrorMsg: "must contain a resource",
		},
		{
			Name: "bad resource",
			Rec: &Recommendation{
				Value:     10,
				Group:     "web",
				Task:      "nginx",
				Resource:  "Memory",
				Region:    "global",
				Namespace: "default",
				JobID:     "example",
			},
			Error:    true,
			ErrorMsg: "resource not supported",
		},
		{
			Name: "missing job",
			Rec: &Recommendation{
				Value:     10,
				Group:     "web",
				Task:      "nginx",
				Resource:  "CPU",
				Region:    "global",
				Namespace: "default",
			},
			Error:    true,
			ErrorMsg: "must specify a job ID",
		},
		{
			Name: "missing namespace",
			Rec: &Recommendation{
				Value:    10,
				Group:    "web",
				Task:     "nginx",
				Resource: "CPU",
				Region:   "global",
				JobID:    "example",
			},
			Error:    true,
			ErrorMsg: "must specify a job namespace",
		},
		{
			Name: "minimum CPU",
			Rec: &Recommendation{
				Value:     0,
				Group:     "web",
				Task:      "nginx",
				Resource:  "CPU",
				Region:    "global",
				JobID:     "example",
				Namespace: "default",
			},
			Error:    true,
			ErrorMsg: "minimum CPU value",
		},
		{
			Name: "minimum memory",
			Rec: &Recommendation{
				Value:     0,
				Group:     "web",
				Task:      "nginx",
				Resource:  "MemoryMB",
				Region:    "global",
				JobID:     "example",
				Namespace: "default",
			},
			Error:    true,
			ErrorMsg: "minimum MemoryMB value",
		},
		{
			Name: "happy little recommendation",
			Rec: &Recommendation{
				Value:     100,
				Group:     "web",
				Task:      "nginx",
				Resource:  "CPU",
				Region:    "global",
				Namespace: "default",
				JobID:     "example",
			},
			Error: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			err := tc.Rec.Validate()
			assert.Equal(t, tc.Error, err != nil)
			if err != nil {
				assert.Contains(t, err.Error(), tc.ErrorMsg)
			}
		})
	}
}

func TestRecommendation_UpdateJob(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	var rec *Recommendation

	require.NoError(rec.UpdateJob(nil))

	job := &Job{
		Region:    "global",
		ID:        fmt.Sprintf("mock-service-%s", uuid.Generate()),
		Name:      "my-job",
		Namespace: DefaultNamespace,
		Type:      JobTypeService,
		TaskGroups: []*TaskGroup{
			{
				Name: "web",
				Tasks: []*Task{
					{
						Name:   "web",
						Driver: "exec",
						Config: map[string]interface{}{
							"command": "/bin/date",
						},
						Resources: &Resources{
							CPU:      500,
							MemoryMB: 256,
						},
					},
				},
			},
		},
		Status:  JobStatusPending,
		Version: 0,
	}
	rec = &Recommendation{
		ID:         uuid.Generate(),
		Region:     job.Region,
		Namespace:  job.Namespace,
		JobID:      job.ID,
		JobVersion: 0,
	}
	rec.Target(job.TaskGroups[0].Name, job.TaskGroups[0].Tasks[0].Name, "CPU")
	rec.Value = 750
	require.NoError(rec.UpdateJob(job))
	require.Equal(rec.Value, job.LookupTaskGroup(rec.Group).LookupTask(rec.Task).Resources.CPU)

	rec.Resource = "MemoryMB"
	rec.Value = 2048
	require.NoError(rec.UpdateJob(job))
	require.Equal(rec.Value, job.LookupTaskGroup(rec.Group).LookupTask(rec.Task).Resources.MemoryMB)

	rec.Resource = "Bad Resource"
	err := rec.UpdateJob(job)
	require.Error(err)
	require.Contains(err.Error(), "resource not valid")

	rec.Target("bad group", job.TaskGroups[0].Tasks[0].Name, "CPU")
	err = rec.UpdateJob(job)
	require.Error(err)
	require.Contains(err.Error(), "task group does not exist in job")

	rec.Target(job.TaskGroups[0].Name, "bad task", "CPU")
	err = rec.UpdateJob(job)
	require.Error(err)
	require.Contains(err.Error(), "task does not exist in group")

	rec.JobID = "wrong"
	err = rec.UpdateJob(job)
	require.Error(err)
	require.Contains(err.Error(), "recommendation does not match job ID")

	rec.JobID = job.ID
	rec.Namespace = "wrong"
	err = rec.UpdateJob(job)
	require.Error(err)
	require.Contains(err.Error(), "recommendation does not match job namespace")
}
