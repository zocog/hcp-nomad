//go:build ent
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
							CPU:         5000,
							MemoryMB:    2000,
							MemoryMaxMB: 3000,
						},
					},
				},
			},
		},
		{
			Name: "valid, with inf limits",
			Spec: &QuotaSpec{
				Name:        "foo",
				Description: "limit foo",
				Limits: []*QuotaLimit{
					{
						Region: "global",
						RegionLimit: &Resources{
							CPU:         5000,
							MemoryMB:    -1,
							MemoryMaxMB: -1,
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
			Name: "bad limit: too low max memory",
			Spec: &QuotaSpec{
				Name:        "foo",
				Description: "limit foo",
				Limits: []*QuotaLimit{
					{
						Region: "global",
						RegionLimit: &Resources{
							CPU:         5000,
							MemoryMB:    3000,
							MemoryMaxMB: 2000,
						},
					},
				},
			},
			Errors: []string{
				"quota memory_max (2000) cannot be lower than memory (3000)",
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
				"can not limit networks",
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
	quota := &QuotaLimit{
		Region: "foo",
		RegionLimit: &Resources{
			CPU:         1000,
			MemoryMB:    1000,
			MemoryMaxMB: 2000,
		},
	}
	noMemMaxQuota := &QuotaLimit{
		Region: "foo",
		RegionLimit: &Resources{
			CPU:         1000,
			MemoryMB:    1000,
			MemoryMaxMB: -1,
		},
	}

	cases := []struct {
		name  string
		quota *QuotaLimit
		usage *Resources

		eSuperset   bool
		eDimensions []string
	}{
		{
			name:  "under limit",
			quota: quota,
			usage: &Resources{
				CPU:         500,
				MemoryMB:    1000,
				MemoryMaxMB: 1500,
			},
			eSuperset:   true,
			eDimensions: nil,
		},
		{
			name: "unlimited",
			quota: &QuotaLimit{
				Region: "foo",
				RegionLimit: &Resources{
					CPU:         1000,
					MemoryMB:    0,
					MemoryMaxMB: 0,
				},
			},
			usage: &Resources{
				CPU:         500,
				MemoryMB:    1000,
				MemoryMaxMB: 4000,
			},
			eSuperset:   true,
			eDimensions: nil,
		},
		{
			name:  "over: cpu",
			quota: quota,
			usage: &Resources{
				CPU:      1500,
				MemoryMB: 1000,
			},
			eSuperset:   false,
			eDimensions: []string{"cpu"},
		},
		{
			name:  "over: memory",
			quota: quota,
			usage: &Resources{
				CPU:      500,
				MemoryMB: 2000,
			},
			eSuperset:   false,
			eDimensions: []string{"memory"},
		},
		{
			name:  "over: memory_max",
			quota: quota,
			usage: &Resources{
				CPU:         500,
				MemoryMB:    1000,
				MemoryMaxMB: 4000,
			},
			eSuperset:   false,
			eDimensions: []string{"memory_max"},
		},
		{
			name:  "without memory_max: under limit",
			quota: noMemMaxQuota,
			usage: &Resources{
				CPU:         500,
				MemoryMB:    1000,
				MemoryMaxMB: 1000,
			},
			eSuperset:   true,
			eDimensions: nil,
		},
		{
			name:  "without memory_max: over",
			quota: noMemMaxQuota,
			usage: &Resources{
				CPU:         500,
				MemoryMB:    1000,
				MemoryMaxMB: 2000,
			},
			eSuperset:   false,
			eDimensions: []string{"memory_max"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := c.quota.Copy()
			u.RegionLimit = c.usage

			superset, dimensions := c.quota.Superset(u)

			found := []string{}
			for _, d := range dimensions {
				if parts := strings.Split(d, " "); len(parts) != 0 {
					found = append(found, parts[0])
				} else {
					found = append(found, d)
				}
			}
			sort.Strings(found)

			if len(c.eDimensions) == 0 {
				require.Emptyf(t, found, "found: %v", dimensions)
			} else {
				require.Equalf(t, c.eDimensions, found, "found: %v", dimensions)
			}
			require.Equal(t, c.eSuperset, superset)
		})
	}
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

func TestScalingPolicy_Validate_Ent(t *testing.T) {
	type testCase struct {
		name        string
		input       *ScalingPolicy
		expectedErr string
	}

	cases := []testCase{
		{
			name: "full vertical_mem policy",
			input: &ScalingPolicy{
				Policy: map[string]interface{}{
					"key": "value",
				},
				Type:    ScalingPolicyTypeVerticalMem,
				Min:     5,
				Max:     5,
				Enabled: true,
				Target: map[string]string{
					ScalingTargetNamespace: "my-namespace",
					ScalingTargetJob:       "my-job",
					ScalingTargetGroup:     "my-task-group",
					ScalingTargetTask:      "my-task",
				},
			},
		},
		{
			name: "full vertical_cpu policy",
			input: &ScalingPolicy{
				Policy: map[string]interface{}{
					"key": "value",
				},
				Type:    ScalingPolicyTypeVerticalCPU,
				Min:     5,
				Max:     5,
				Enabled: true,
				Target: map[string]string{
					ScalingTargetNamespace: "my-namespace",
					ScalingTargetJob:       "my-job",
					ScalingTargetGroup:     "my-task-group",
					ScalingTargetTask:      "my-task",
				},
			},
		},
		{
			name: "invalid type",
			input: &ScalingPolicy{
				Type: "not valid",
			},
			expectedErr: `scaling policy type "not valid" is not valid`,
		},
		{
			name: "missing target vertical_mem",
			input: &ScalingPolicy{
				Type: ScalingPolicyTypeVerticalMem,
			},
			expectedErr: "vertical_mem policies must have a target",
		},
		{
			name: "missing target vertical_cpu",
			input: &ScalingPolicy{
				Type: ScalingPolicyTypeVerticalCPU,
			},
			expectedErr: "vertical_cpu policies must have a target",
		},
	}

	// Explicitly build valid vertical policy target cases.
	for _, t := range []string{ScalingPolicyTypeVerticalMem, ScalingPolicyTypeVerticalCPU} {
		cases = append(cases, testCase{
			name: fmt.Sprintf("%s target namespace", t),
			input: &ScalingPolicy{
				Type: t,
				Target: map[string]string{
					ScalingTargetNamespace: "my-namespace",
				},
			},
		})
		cases = append(cases, testCase{
			name: fmt.Sprintf("%s target job", t),
			input: &ScalingPolicy{
				Type: t,
				Target: map[string]string{
					ScalingTargetNamespace: "my-namespace",
					ScalingTargetJob:       "my-job",
				},
			},
		})
		cases = append(cases, testCase{
			name: fmt.Sprintf("%s target group", t),
			input: &ScalingPolicy{
				Type: t,
				Target: map[string]string{
					ScalingTargetNamespace: "my-namespace",
					ScalingTargetJob:       "my-job",
					ScalingTargetGroup:     "my-group",
				},
			},
		})
		cases = append(cases, testCase{
			name: fmt.Sprintf("%s target task", t),
			input: &ScalingPolicy{
				Type: t,
				Target: map[string]string{
					ScalingTargetNamespace: "my-namespace",
					ScalingTargetJob:       "my-job",
					ScalingTargetGroup:     "my-group",
					ScalingTargetTask:      "my-task",
				},
			},
		})
	}

	// Generate invalid vertical policy target cases.
	// Each case is defined as a 4-bit binary number, with each bit representing
	// a field (Namespace, Job, Group, Task) and a bit 1 meaning the field is set.
	// The valid cases are 1000,  1100, 1110, 1111 so we skip them.
	valid := []int{0b1000, 0b1100, 0b1110, 0b1111}
	isValid := func(i int) bool {
		for _, v := range valid {
			if i == v {
				return true
			}
		}
		return false
	}

	// Create bitmasks to check which bits are set.
	var (
		namespaceMask = 0b1000
		jobMask       = 0b0100
		groupMask     = 0b0010
		taskMask      = 0b0001
	)

	// Start from 0001 because 0000 is an empty target, which returns a different
	// error message. We also already test for it.
	for i := 0b0001; i <= 0b1111; i++ {
		if isValid(i) {
			continue
		}

		target := make(map[string]string)

		// Apply bitmasks and set the value on fields with bit 1.
		if i&namespaceMask != 0 {
			target[ScalingTargetNamespace] = "my-namespace"
		}
		if i&jobMask != 0 {
			target[ScalingTargetJob] = "my-job"
		}
		if i&groupMask != 0 {
			target[ScalingTargetGroup] = "my-group"
		}
		if i&taskMask != 0 {
			target[ScalingTargetTask] = "my-task"
		}

		// Create cases for multiple types.
		for _, t := range []string{ScalingPolicyTypeVerticalMem, ScalingPolicyTypeVerticalCPU} {
			cases = append(cases, testCase{
				name: fmt.Sprintf("%s invalid target %b", t, i),
				input: &ScalingPolicy{
					Type:   t,
					Target: target,
				},
				expectedErr: "missing target",
			})
		}
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require := require.New(t)

			err := c.input.Validate()

			if len(c.expectedErr) > 0 {
				require.Error(err)
				mErr := err.(*multierror.Error)
				require.Len(mErr.Errors, 1)
				require.Contains(mErr.Errors[0].Error(), c.expectedErr)
			} else {
				require.NoError(err)
			}
		})
	}
}

func TestJob_GetScalingPolicies_TaskPolicies(t *testing.T) {
	t.Parallel()

	job := MockJob()
	var expected []*ScalingPolicy

	// no policies
	t.Run("job with no policies", func(t *testing.T) {
		actual := job.GetScalingPolicies()
		assert.ElementsMatch(t, expected, actual)
	})

	// one group policy
	pGroup := &ScalingPolicy{
		ID:      uuid.Generate(),
		Type:    ScalingPolicyTypeHorizontal,
		Policy:  nil,
		Min:     5,
		Max:     10,
		Enabled: true,
	}
	pGroup.TargetTaskGroup(job, job.TaskGroups[0])
	job.TaskGroups[0].Scaling = pGroup
	expected = append(expected, pGroup)
	t.Run("job with single group policy", func(t *testing.T) {
		actual := job.GetScalingPolicies()
		assert.ElementsMatch(t, expected, actual)
	})

	// plus a task policy
	pTaskCpu := &ScalingPolicy{
		ID:      uuid.Generate(),
		Type:    ScalingPolicyTypeVerticalCPU,
		Policy:  nil,
		Min:     128,
		Max:     256,
		Enabled: true,
	}
	pTaskCpu.TargetTask(job, job.TaskGroups[0], job.TaskGroups[0].Tasks[0])
	job.TaskGroups[0].Tasks[0].ScalingPolicies = []*ScalingPolicy{pTaskCpu}
	expected = append(expected, pTaskCpu)
	t.Run("job with a task policy", func(t *testing.T) {
		actual := job.GetScalingPolicies()
		assert.ElementsMatch(t, expected, actual)
	})

	// plus one more task policy
	pTaskMem := &ScalingPolicy{
		ID:      uuid.Generate(),
		Type:    ScalingPolicyTypeVerticalMem,
		Policy:  nil,
		Min:     1024,
		Max:     2048,
		Enabled: true,
	}
	pTaskMem.TargetTask(job, job.TaskGroups[0], job.TaskGroups[0].Tasks[0])
	job.TaskGroups[0].Tasks[0].ScalingPolicies = []*ScalingPolicy{pTaskCpu, pTaskMem}
	expected = append(expected, pTaskMem)
	t.Run("job with multiple task policies", func(t *testing.T) {
		actual := job.GetScalingPolicies()
		assert.ElementsMatch(t, expected, actual)
	})
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
