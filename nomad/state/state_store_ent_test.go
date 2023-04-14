//go:build ent
// +build ent

package state

import (
	"sort"
	"testing"
	"time"

	memdb "github.com/hashicorp/go-memdb"
	"github.com/hashicorp/nomad/ci"
	"github.com/shoenig/test/must"

	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
)

func TestStateStore_UpsertSentinelPolicy(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	policy := mock.SentinelPolicy()
	policy2 := mock.SentinelPolicy()

	ws := memdb.NewWatchSet()
	if _, err := state.SentinelPolicyByName(ws, policy.Name); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := state.SentinelPolicyByName(ws, policy2.Name); err != nil {
		t.Fatalf("err: %v", err)
	}

	if err := state.UpsertSentinelPolicies(1000,
		[]*structs.SentinelPolicy{policy, policy2}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !watchFired(ws) {
		t.Fatalf("bad")
	}

	ws = memdb.NewWatchSet()
	out, err := state.SentinelPolicyByName(ws, policy.Name)
	must.Eq(t, nil, err)
	must.Eq(t, policy, out)

	out, err = state.SentinelPolicyByName(ws, policy2.Name)
	must.Eq(t, nil, err)
	must.Eq(t, policy2, out)

	iter, err := state.SentinelPolicies(ws)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Ensure we see both policies
	count := 0
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		count++
	}
	if count != 2 {
		t.Fatalf("bad: %d", count)
	}

	iter, err = state.SentinelPoliciesByScope(ws, "submit-job")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Ensure we see both policies
	count = 0
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		count++
	}
	if count != 2 {
		t.Fatalf("bad: %d", count)
	}

	index, err := state.Index("sentinel_policy")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if index != 1000 {
		t.Fatalf("bad: %d", index)
	}

	if watchFired(ws) {
		t.Fatalf("bad")
	}
}

func TestStateStore_DeleteSentinelPolicy(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	policy := mock.SentinelPolicy()
	policy2 := mock.SentinelPolicy()

	// Create the policy
	if err := state.UpsertSentinelPolicies(1000,
		[]*structs.SentinelPolicy{policy, policy2}); err != nil {
		t.Fatalf("err: %v", err)
	}

	// Create a watcher
	ws := memdb.NewWatchSet()
	if _, err := state.SentinelPolicyByName(ws, policy.Name); err != nil {
		t.Fatalf("err: %v", err)
	}

	// Delete the policy
	if err := state.DeleteSentinelPolicies(1001,
		[]string{policy.Name, policy2.Name}); err != nil {
		t.Fatalf("err: %v", err)
	}

	// Ensure watching triggered
	if !watchFired(ws) {
		t.Fatalf("bad")
	}

	// Ensure we don't get the object back
	ws = memdb.NewWatchSet()
	out, err := state.SentinelPolicyByName(ws, policy.Name)
	must.Eq(t, nil, err)
	if out != nil {
		t.Fatalf("bad: %#v", out)
	}

	iter, err := state.SentinelPolicies(ws)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Ensure we see both policies
	count := 0
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		count++
	}
	if count != 0 {
		t.Fatalf("bad: %d", count)
	}

	index, err := state.Index("sentinel_policy")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if index != 1001 {
		t.Fatalf("bad: %d", index)
	}

	if watchFired(ws) {
		t.Fatalf("bad")
	}
}

func TestStateStore_SentinelPolicyByNamePrefix(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	names := []string{
		"foo",
		"bar",
		"foobar",
		"foozip",
		"zip",
	}

	// Create the policies
	var baseIndex uint64 = 1000
	for _, name := range names {
		p := mock.SentinelPolicy()
		p.Name = name
		if err := state.UpsertSentinelPolicies(baseIndex, []*structs.SentinelPolicy{p}); err != nil {
			t.Fatalf("err: %v", err)
		}
		baseIndex++
	}

	// Scan by prefix
	iter, err := state.SentinelPolicyByNamePrefix(nil, "foo")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Ensure we see both policies
	count := 0
	out := []string{}
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		count++
		out = append(out, raw.(*structs.SentinelPolicy).Name)
	}
	if count != 3 {
		t.Fatalf("bad: %d %v", count, out)
	}
	sort.Strings(out)

	expect := []string{"foo", "foobar", "foozip"}
	must.Eq(t, expect, out)
}

func TestStateStore_RestoreSentinelPolicy(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	policy := mock.SentinelPolicy()

	restore, err := state.Restore()
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	err = restore.SentinelPolicyRestore(policy)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	restore.Commit()

	ws := memdb.NewWatchSet()
	out, err := state.SentinelPolicyByName(ws, policy.Name)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	must.Eq(t, policy, out)
}

func TestStateStore_NamespaceByQuota(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	qs := mock.QuotaSpec()
	must.Nil(t, state.UpsertQuotaSpecs(999, []*structs.QuotaSpec{qs}))

	ns1 := mock.Namespace()
	ns2 := mock.Namespace()
	ns2.Quota = qs.Name
	must.Nil(t, state.UpsertNamespaces(1000, []*structs.Namespace{ns1, ns2}))

	// Create a watchset so we can test that getters don't cause it to fire
	ws := memdb.NewWatchSet()
	iter, err := state.NamespacesByQuota(ws, ns2.Quota)
	must.Nil(t, err)

	gatherNamespaces := func(iter memdb.ResultIterator) []*structs.Namespace {
		var namespaces []*structs.Namespace
		for {
			raw := iter.Next()
			if raw == nil {
				break
			}
			ns := raw.(*structs.Namespace)
			namespaces = append(namespaces, ns)
		}
		return namespaces
	}

	namespaces := gatherNamespaces(iter)
	must.Len(t, 1, namespaces)
	must.Eq(t, ns2.Name, namespaces[0].Name)
	must.False(t, watchFired(ws))

	iter, err = state.NamespacesByQuota(ws, "bar")
	must.Nil(t, err)

	namespaces = gatherNamespaces(iter)
	must.SliceEmpty(t, namespaces)
}

func TestStateStore_UpsertAllocs_Quota_NewAlloc(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	// 1. Create a QuotaSpec
	qs := mock.QuotaSpec()
	must.Nil(t, state.UpsertQuotaSpecs(1000, []*structs.QuotaSpec{qs}))

	// 2. Create a namespace with a quota
	ns1 := mock.Namespace()
	ns1.Quota = qs.Name
	must.Nil(t, state.UpsertNamespaces(1001, []*structs.Namespace{ns1}))

	// 3. Create some allocations in the namespace
	a1 := mock.Alloc()
	a2 := mock.Alloc()
	a1.Namespace = ns1.Name
	a2.Namespace = ns1.Name
	allocs := []*structs.Allocation{a1, a2}
	must.Nil(t, state.UpsertAllocs(structs.MsgTypeTestSetup, 1002, allocs))

	// 4. Assert that the QuotaUsage is updated.
	usage, err := state.QuotaUsageByName(nil, qs.Name)
	must.Nil(t, err)
	must.NotNil(t, usage)
	must.Eq(t, 1000, usage.CreateIndex)
	must.Eq(t, 1002, usage.ModifyIndex)
	must.MapLen(t, 1, usage.Used)

	// Grab the usage
	used := usage.Used[string(qs.Limits[0].Hash)]
	must.NotNil(t, used)
	must.Eq(t, "global", used.Region)

	expected := &structs.Resources{}
	r := mock.Alloc().Resources
	expected.Add(r)
	expected.Add(r)
	expected.Networks = nil
	expected.DiskMB = 0
	expected.IOPS = 0
	must.Eq(t, expected, used.RegionLimit)
}

// This should no-op
func TestStateStore_UpsertAllocs_Quota_UpdateAlloc(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	// 1. Create a QuotaSpec
	qs := mock.QuotaSpec()
	must.Nil(t, state.UpsertQuotaSpecs(1000, []*structs.QuotaSpec{qs}))

	// 2. Create a namespace with a quota
	ns1 := mock.Namespace()
	ns1.Quota = qs.Name
	must.Nil(t, state.UpsertNamespaces(1001, []*structs.Namespace{ns1}))

	// 3. Create some allocations in the namespace
	a1 := mock.Alloc()
	a2 := mock.Alloc()
	a1.Namespace = ns1.Name
	a2.Namespace = ns1.Name
	allocs := []*structs.Allocation{a1, a2}
	must.Nil(t, state.UpsertAllocs(structs.MsgTypeTestSetup, 1002, allocs))

	// 4. Get the QuotaUsage
	usageOriginal, err := state.QuotaUsageByName(nil, qs.Name)
	must.Nil(t, err)
	must.NotNil(t, usageOriginal)
	must.Eq(t, 1000, usageOriginal.CreateIndex)
	must.Eq(t, 1002, usageOriginal.ModifyIndex)
	must.MapLen(t, 1, usageOriginal.Used)

	// Grab the usage
	usedOriginal := usageOriginal.Used[string(qs.Limits[0].Hash)]
	must.NotNil(t, usedOriginal)

	// 5. Update the allocs
	j := mock.Alloc().Job
	j.Meta = map[string]string{"foo": "bar"}
	a3 := a1.Copy()
	a4 := a2.Copy()
	a3.Job = j
	a4.Job = j
	allocs = []*structs.Allocation{a3, a4}
	must.Nil(t, state.UpsertAllocs(structs.MsgTypeTestSetup, 1003, allocs))

	// 6. Assert that the QuotaUsage is not updated.
	usageUpdated, err := state.QuotaUsageByName(nil, qs.Name)
	must.Nil(t, err)
	must.NotNil(t, usageUpdated)
	must.Eq(t, 1000, usageUpdated.CreateIndex)
	must.Eq(t, 1002, usageUpdated.ModifyIndex)
	must.MapLen(t, 1, usageUpdated.Used)

	// Grab the usage
	usedUpdated := usageUpdated.Used[string(qs.Limits[0].Hash)]
	must.NotNil(t, usedUpdated)
	must.Eq(t, "global", usedUpdated.Region)

	must.Eq(t, usedOriginal.RegionLimit, usedUpdated.RegionLimit)
}

func TestStateStore_UpsertAllocs_Quota_StopAlloc(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	// 1. Create a QuotaSpec
	qs := mock.QuotaSpec()
	must.Nil(t, state.UpsertQuotaSpecs(1000, []*structs.QuotaSpec{qs}))

	// 2. Create a namespace with a quota
	ns1 := mock.Namespace()
	ns1.Quota = qs.Name
	must.Nil(t, state.UpsertNamespaces(1001, []*structs.Namespace{ns1}))

	// 3. Create some allocations in the namespace
	a1 := mock.Alloc()
	a2 := mock.Alloc()
	a1.Namespace = ns1.Name
	a2.Namespace = ns1.Name
	allocs := []*structs.Allocation{a1, a2}
	must.Nil(t, state.UpsertAllocs(structs.MsgTypeTestSetup, 1002, allocs))

	// 4. Stop the allocs
	a3 := a1.Copy()
	a4 := a2.Copy()
	a3.DesiredStatus = structs.AllocDesiredStatusStop
	a4.DesiredStatus = structs.AllocDesiredStatusStop
	allocs = []*structs.Allocation{a3, a4}
	must.Nil(t, state.UpsertAllocs(structs.MsgTypeTestSetup, 1003, allocs))

	// 5. Assert that the QuotaUsage is updated.
	usage, err := state.QuotaUsageByName(nil, qs.Name)
	must.Nil(t, err)
	must.NotNil(t, usage)
	must.Eq(t, 1000, usage.CreateIndex)
	must.Eq(t, 1003, usage.ModifyIndex)
	must.MapLen(t, 1, usage.Used)

	// Grab the usage
	used := usage.Used[string(qs.Limits[0].Hash)]
	must.NotNil(t, used)
	must.Eq(t, "global", used.Region)
	expected := &structs.Resources{}
	must.Eq(t, expected, used.RegionLimit)
}

// This should no-op
func TestStateStore_UpdateAllocsFromClient_Quota_UpdateAlloc(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	// 1. Create a QuotaSpec
	qs := mock.QuotaSpec()
	must.Nil(t, state.UpsertQuotaSpecs(1000, []*structs.QuotaSpec{qs}))

	// 2. Create a namespace with a quota
	ns1 := mock.Namespace()
	ns1.Quota = qs.Name
	must.Nil(t, state.UpsertNamespaces(1001, []*structs.Namespace{ns1}))

	// 3. Create some allocations in the namespace
	a1 := mock.Alloc()
	a2 := mock.Alloc()
	a1.Namespace = ns1.Name
	a2.Namespace = ns1.Name
	allocs := []*structs.Allocation{a1, a2}
	must.Nil(t, state.UpsertAllocs(structs.MsgTypeTestSetup, 1002, allocs))

	// 4. Get the QuotaUsage
	usageOriginal, err := state.QuotaUsageByName(nil, qs.Name)
	must.Nil(t, err)
	must.NotNil(t, usageOriginal)
	must.Eq(t, 1000, usageOriginal.CreateIndex)
	must.Eq(t, 1002, usageOriginal.ModifyIndex)
	must.MapLen(t, 1, usageOriginal.Used)

	// Grab the usage
	usedOriginal := usageOriginal.Used[string(qs.Limits[0].Hash)]
	must.NotNil(t, usedOriginal)

	// 5. Update the allocs
	a3 := a1.Copy()
	a4 := a2.Copy()
	a3.ClientStatus = structs.AllocClientStatusRunning
	a4.ClientStatus = structs.AllocClientStatusRunning
	allocs = []*structs.Allocation{a3, a4}
	must.Nil(t, state.UpdateAllocsFromClient(structs.MsgTypeTestSetup, 1003, allocs))

	// 6. Assert that the QuotaUsage is not updated.
	usageUpdated, err := state.QuotaUsageByName(nil, qs.Name)
	must.Nil(t, err)
	must.NotNil(t, usageUpdated)
	must.Eq(t, 1000, usageUpdated.CreateIndex)
	must.Eq(t, 1002, usageUpdated.ModifyIndex)
	must.MapLen(t, 1, usageUpdated.Used)

	// Grab the usage
	usedUpdated := usageUpdated.Used[string(qs.Limits[0].Hash)]
	must.NotNil(t, usedUpdated)
	must.Eq(t, "global", usedUpdated.Region)

	must.Eq(t, usedOriginal.RegionLimit, usedUpdated.RegionLimit)
}

func TestStateStore_UpdateAllocsFromClient_Quota_StopAlloc(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	// 1. Create a QuotaSpec
	qs := mock.QuotaSpec()
	must.Nil(t, state.UpsertQuotaSpecs(1000, []*structs.QuotaSpec{qs}))

	// 2. Create a namespace with a quota
	ns1 := mock.Namespace()
	ns1.Quota = qs.Name
	must.Nil(t, state.UpsertNamespaces(1001, []*structs.Namespace{ns1}))

	// 3. Create some allocations in the namespace
	a1 := mock.Alloc()
	a2 := mock.Alloc()
	a1.Namespace = ns1.Name
	a2.Namespace = ns1.Name
	allocs := []*structs.Allocation{a1, a2}
	must.Nil(t, state.UpsertAllocs(structs.MsgTypeTestSetup, 1002, allocs))

	// 4. Stop the allocs
	a3 := a1.Copy()
	a4 := a2.Copy()
	a3.ClientStatus = structs.AllocClientStatusFailed
	a4.ClientStatus = structs.AllocClientStatusFailed
	allocs = []*structs.Allocation{a3, a4}
	must.Nil(t, state.UpdateAllocsFromClient(structs.MsgTypeTestSetup, 1003, allocs))

	// 5. Assert that the QuotaUsage is updated.
	usage, err := state.QuotaUsageByName(nil, qs.Name)
	must.Nil(t, err)
	must.NotNil(t, usage)
	must.Eq(t, 1000, usage.CreateIndex)
	must.Eq(t, 1003, usage.ModifyIndex)
	must.MapLen(t, 1, usage.Used)

	// Grab the usage
	used := usage.Used[string(qs.Limits[0].Hash)]
	must.NotNil(t, used)
	must.Eq(t, "global", used.Region)
	expected := &structs.Resources{}
	must.Eq(t, expected, used.RegionLimit)
}

func TestStateStore_UpsertNamespaces_BadQuota(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	ns1 := mock.Namespace()
	ns1.Quota = "foo"
	must.NotNil(t, state.UpsertNamespaces(1000, []*structs.Namespace{ns1}))
}

func TestStateStore_UpsertNamespaces_NewQuota(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	// 1. Create a namespace
	ns1 := mock.Namespace()
	must.Nil(t, state.UpsertNamespaces(1000, []*structs.Namespace{ns1}))

	// expected is the expected quota usage
	expected := &structs.Resources{}

	// 2. Create some allocations in the namespace
	var allocs []*structs.Allocation

	// Create a pending alloc
	a1 := mock.Alloc()
	a1.DesiredStatus = structs.AllocDesiredStatusRun
	a1.ClientStatus = structs.AllocClientStatusPending
	a1.Namespace = ns1.Name
	expected.Add(a1.Resources)

	// Create a running alloc
	a2 := mock.Alloc()
	a2.DesiredStatus = structs.AllocDesiredStatusRun
	a2.ClientStatus = structs.AllocClientStatusRunning
	a2.Namespace = ns1.Name
	expected.Add(a2.Resources)

	// Create a run/complete alloc
	a3 := mock.Alloc()
	a3.DesiredStatus = structs.AllocDesiredStatusRun
	a3.ClientStatus = structs.AllocClientStatusComplete
	a3.Namespace = ns1.Name
	allocs = append(allocs, a1, a2, a3)
	must.Nil(t, state.UpsertAllocs(structs.MsgTypeTestSetup, 1001, allocs))

	// 3. Create a QuotaSpec and attach it to the namespace
	qs := mock.QuotaSpec()
	must.Nil(t, state.UpsertQuotaSpecs(1002, []*structs.QuotaSpec{qs}))
	ns2 := mock.Namespace()
	ns2.Name = ns1.Name
	ns2.Quota = qs.Name
	ns2.SetHash()
	must.Nil(t, state.UpsertNamespaces(1003, []*structs.Namespace{ns2}))

	// 4. Assert that the QuotaUsage is updated.
	usage, err := state.QuotaUsageByName(nil, qs.Name)
	must.Nil(t, err)
	must.NotNil(t, usage)
	must.Eq(t, 1002, usage.CreateIndex)
	must.Eq(t, 1003, usage.ModifyIndex)
	must.MapLen(t, 1, usage.Used)

	// Grab the usage
	used := usage.Used[string(qs.Limits[0].Hash)]
	must.NotNil(t, used)
	must.Eq(t, "global", used.Region)

	// Clear fields unused by QuotaLimits
	expected.Networks = nil
	expected.DiskMB = 0
	expected.IOPS = 0
	must.Eq(t, expected, used.RegionLimit)

}

func TestStateStore_UpsertNamespaces_RemoveQuota(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	// 1. Create a QuotaSpec
	qs := mock.QuotaSpec()
	must.Nil(t, state.UpsertQuotaSpecs(1000, []*structs.QuotaSpec{qs}))

	// 2. Create a namespace
	ns1 := mock.Namespace()
	must.Nil(t, state.UpsertNamespaces(1001, []*structs.Namespace{ns1}))

	// 3. Create a allocation in the namespace
	a1 := mock.Alloc()
	a1.DesiredStatus = structs.AllocDesiredStatusRun
	a1.ClientStatus = structs.AllocClientStatusPending
	a1.Namespace = ns1.Name
	must.Nil(t, state.UpsertAllocs(structs.MsgTypeTestSetup, 1002, []*structs.Allocation{a1}))

	// 4. Create a QuotaSpec and attach it to the namespace
	ns2 := mock.Namespace()
	ns2.Name = ns1.Name
	ns2.Quota = qs.Name
	ns2.SetHash()
	must.Nil(t, state.UpsertNamespaces(1003, []*structs.Namespace{ns2}))

	// 5. Remove the spec from the namespace
	ns3 := mock.Namespace()
	ns3.Name = ns1.Name
	ns3.SetHash()
	must.Nil(t, state.UpsertNamespaces(1004, []*structs.Namespace{ns3}))

	// 6. Assert that the QuotaUsage is empty.
	usage, err := state.QuotaUsageByName(nil, qs.Name)
	must.Nil(t, err)
	must.NotNil(t, usage)
	must.Eq(t, 1000, usage.CreateIndex)
	must.Eq(t, 1004, usage.ModifyIndex)
	must.MapLen(t, 1, usage.Used)

	// Grab the usage
	used := usage.Used[string(qs.Limits[0].Hash)]
	must.NotNil(t, used)
	must.Eq(t, "global", used.Region)
	expected := &structs.Resources{}
	must.Eq(t, expected, used.RegionLimit)
}

func TestStateStore_UpsertNamespaces_ChangeQuota(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	// 1. Create two QuotaSpecs
	qs1 := mock.QuotaSpec()
	qs2 := mock.QuotaSpec()
	must.Nil(t, state.UpsertQuotaSpecs(1000, []*structs.QuotaSpec{qs1, qs2}))

	// 2. Create a namespace
	ns1 := mock.Namespace()
	must.Nil(t, state.UpsertNamespaces(1001, []*structs.Namespace{ns1}))

	// 3. Create a allocation in the namespace
	a1 := mock.Alloc()
	a1.DesiredStatus = structs.AllocDesiredStatusRun
	a1.ClientStatus = structs.AllocClientStatusPending
	a1.Namespace = ns1.Name
	must.Nil(t, state.UpsertAllocs(structs.MsgTypeTestSetup, 1002, []*structs.Allocation{a1}))

	// 4. Create a QuotaSpec and attach it to the namespace
	ns2 := mock.Namespace()
	ns2.Name = ns1.Name
	ns2.Quota = qs1.Name
	ns2.SetHash()
	must.Nil(t, state.UpsertNamespaces(1003, []*structs.Namespace{ns2}))

	// 5. Change the spec on the namespace
	ns3 := mock.Namespace()
	ns3.Name = ns1.Name
	ns3.Quota = qs2.Name
	ns3.SetHash()
	must.Nil(t, state.UpsertNamespaces(1004, []*structs.Namespace{ns3}))

	// 6. Assert that the QuotaUsage for original spec is empty.
	usage, err := state.QuotaUsageByName(nil, qs1.Name)
	must.Nil(t, err)
	must.NotNil(t, usage)
	must.Eq(t, 1000, usage.CreateIndex)
	must.Eq(t, 1004, usage.ModifyIndex)
	must.MapLen(t, 1, usage.Used)

	// Grab the usage
	used := usage.Used[string(qs1.Limits[0].Hash)]
	must.NotNil(t, used)
	must.Eq(t, "global", used.Region)
	expected := &structs.Resources{}
	must.Eq(t, expected, used.RegionLimit)

	// 7. Assert that the QuotaUsage for new spec is populated.
	usage, err = state.QuotaUsageByName(nil, qs2.Name)
	must.Nil(t, err)
	must.NotNil(t, usage)
	must.Eq(t, 1000, usage.CreateIndex)
	must.Eq(t, 1004, usage.ModifyIndex)
	must.MapLen(t, 1, usage.Used)

	// Grab the usage
	used = usage.Used[string(qs2.Limits[0].Hash)]
	must.NotNil(t, used)
	must.Eq(t, "global", used.Region)
	expected.Networks = a1.Resources.Networks
	expected = &structs.Resources{
		CPU:         a1.Resources.CPU,
		MemoryMB:    a1.Resources.MemoryMB,
		MemoryMaxMB: a1.Resources.MemoryMB,
	}
	must.Eq(t, expected, used.RegionLimit)
}

func TestStateStore_UpsertQuotaSpec(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	qs1 := mock.QuotaSpec()
	qs2 := mock.QuotaSpec()

	ws := memdb.NewWatchSet()
	out, err := state.QuotaSpecByName(ws, qs1.Name)
	must.Nil(t, out)
	must.Nil(t, err)
	out, err = state.QuotaSpecByName(ws, qs2.Name)
	must.Nil(t, out)
	must.Nil(t, err)

	must.Nil(t, state.UpsertQuotaSpecs(1000, []*structs.QuotaSpec{qs1, qs2}))
	must.True(t, watchFired(ws))

	ws = memdb.NewWatchSet()
	out, err = state.QuotaSpecByName(ws, qs1.Name)
	must.Nil(t, err)
	must.Eq(t, qs1, out)

	out, err = state.QuotaSpecByName(ws, qs2.Name)
	must.Nil(t, err)
	must.Eq(t, qs2, out)

	// Assert there are corresponding usage objects
	usage, err := state.QuotaUsageByName(ws, qs1.Name)
	must.Nil(t, err)
	must.NotNil(t, usage)

	usage, err = state.QuotaUsageByName(ws, qs2.Name)
	must.Nil(t, err)
	must.NotNil(t, usage)

	iter, err := state.QuotaSpecs(ws)
	must.Nil(t, err)

	// Ensure we see both specs
	count := 0
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		count++
	}
	must.Eq(t, 2, count)

	index, err := state.Index(TableQuotaSpec)
	must.Nil(t, err)
	must.Eq(t, 1000, index)
	must.False(t, watchFired(ws))
}

func TestStateStore_UpsertQuotaSpec_Usage(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	// Create a quota specification with no limits
	qs := mock.QuotaSpec()
	limits := qs.Limits
	qs.Limits = nil
	must.Nil(t, state.UpsertQuotaSpecs(1000, []*structs.QuotaSpec{qs}))

	// Create two namespaces and have one attach the quota specification
	ns1 := mock.Namespace()
	ns1.Quota = qs.Name
	ns2 := mock.Namespace()
	namespaces := []*structs.Namespace{ns1, ns2}
	must.Nil(t, state.UpsertNamespaces(1001, namespaces))

	// expected is the expected quota usage
	expected := &structs.Resources{}

	// Create allocations in various states for both namespaces
	var allocs []*structs.Allocation
	for _, ns := range namespaces {
		// Create a pending alloc
		a1 := mock.Alloc()
		a1.DesiredStatus = structs.AllocDesiredStatusRun
		a1.ClientStatus = structs.AllocClientStatusPending
		a1.Namespace = ns.Name
		if ns.Quota != "" {
			expected.Add(a1.Resources)
		}

		// Create a running alloc
		a2 := mock.Alloc()
		a2.DesiredStatus = structs.AllocDesiredStatusRun
		a2.ClientStatus = structs.AllocClientStatusRunning
		a2.Namespace = ns.Name
		if ns.Quota != "" {
			expected.Add(a2.Resources)
		}

		// Create a run/complete alloc
		a3 := mock.Alloc()
		a3.DesiredStatus = structs.AllocDesiredStatusRun
		a3.ClientStatus = structs.AllocClientStatusComplete
		a3.Namespace = ns.Name

		// Create a stop/complete alloc
		a4 := mock.Alloc()
		a4.DesiredStatus = structs.AllocDesiredStatusStop
		a4.ClientStatus = structs.AllocClientStatusComplete
		a4.Namespace = ns.Name

		// Create a run/failed alloc
		a5 := mock.Alloc()
		a5.DesiredStatus = structs.AllocDesiredStatusRun
		a5.ClientStatus = structs.AllocClientStatusFailed
		a5.Namespace = ns.Name

		// Create a stop/failed alloc
		a6 := mock.Alloc()
		a6.DesiredStatus = structs.AllocDesiredStatusStop
		a6.ClientStatus = structs.AllocClientStatusFailed
		a6.Namespace = ns.Name

		// Create a lost alloc
		a7 := mock.Alloc()
		a7.DesiredStatus = structs.AllocDesiredStatusStop
		a7.ClientStatus = structs.AllocClientStatusLost
		a7.Namespace = ns.Name

		allocs = append(allocs, a1, a2, a3, a4, a5, a6, a7)
	}
	must.Nil(t, state.UpsertAllocs(structs.MsgTypeTestSetup, 1002, allocs))

	// Add limits to the spec
	qs2 := mock.QuotaSpec()
	qs2.Name = qs.Name
	qs2.Limits = limits
	must.Nil(t, state.UpsertQuotaSpecs(1003, []*structs.QuotaSpec{qs2}))

	// Assert the usage is built properly
	usage, err := state.QuotaUsageByName(nil, qs2.Name)
	must.Nil(t, err)
	must.NotNil(t, usage)
	must.Eq(t, 1000, usage.CreateIndex)
	must.Eq(t, 1003, usage.ModifyIndex)
	must.MapLen(t, 1, usage.Used)

	// Grab the usage
	used := usage.Used[string(limits[0].Hash)]
	must.NotNil(t, used)
	must.Eq(t, "global", used.Region)
	expected.Networks = nil
	expected.DiskMB = 0
	expected.IOPS = 0
	must.Eq(t, expected, used.RegionLimit)
}

func TestStateStore_DeleteQuotaSpecs(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	qs1 := mock.QuotaSpec()
	qs2 := mock.QuotaSpec()

	// Create the quota specs
	must.Nil(t, state.UpsertQuotaSpecs(1000, []*structs.QuotaSpec{qs1, qs2}))

	// Create a watcher
	ws := memdb.NewWatchSet()
	_, err := state.QuotaSpecByName(ws, qs1.Name)
	must.Nil(t, err)

	// Delete the spec
	must.Nil(t, state.DeleteQuotaSpecs(1001, []string{qs1.Name, qs2.Name}))

	// Ensure watching triggered
	must.True(t, watchFired(ws))

	// Ensure we don't get the object back or a usage
	ws = memdb.NewWatchSet()
	out, err := state.QuotaSpecByName(ws, qs1.Name)
	must.Nil(t, err)
	must.Nil(t, out)

	usage, err := state.QuotaUsageByName(ws, qs1.Name)
	must.Nil(t, err)
	must.Nil(t, usage)

	iter, err := state.QuotaSpecs(ws)
	must.Nil(t, err)

	// Ensure we see both policies
	count := 0
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		count++
	}
	must.Zero(t, count)

	index, err := state.Index(TableQuotaSpec)
	must.Nil(t, err)
	must.Eq(t, 1001, index)
	must.False(t, watchFired(ws))
}

func TestStateStore_DeleteQuotaSpecs_Referenced(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	qs1 := mock.QuotaSpec()

	// Create the quota specs
	must.Nil(t, state.UpsertQuotaSpecs(1000, []*structs.QuotaSpec{qs1}))

	// Create two namespaces that reference the spec
	ns1, ns2 := mock.Namespace(), mock.Namespace()
	ns1.Quota = qs1.Name
	ns2.Quota = qs1.Name
	must.Nil(t, state.UpsertNamespaces(1001, []*structs.Namespace{ns1, ns2}))

	// Delete the spec
	err := state.DeleteQuotaSpecs(1002, []string{qs1.Name})
	must.NotNil(t, err)
	must.StrContains(t, err.Error(), ns1.Name)
	must.StrContains(t, err.Error(), ns2.Name)
}

func TestStateStore_QuotaSpecsByNamePrefix(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	names := []string{
		"foo",
		"bar",
		"foobar",
		"foozip",
		"zip",
	}

	// Create the policies
	var baseIndex uint64 = 1000
	for _, name := range names {
		qs := mock.QuotaSpec()
		qs.Name = name
		must.Nil(t, state.UpsertQuotaSpecs(baseIndex, []*structs.QuotaSpec{qs}))
		baseIndex++
	}

	// Scan by prefix
	iter, err := state.QuotaSpecsByNamePrefix(nil, "foo")
	must.Nil(t, err)

	// Ensure we see both policies
	count := 0
	out := []string{}
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		count++
		out = append(out, raw.(*structs.QuotaSpec).Name)
	}
	must.Eq(t, 3, count)
	sort.Strings(out)

	expect := []string{"foo", "foobar", "foozip"}
	must.Eq(t, expect, out)
}

func TestStateStore_RestoreQuotaSpec(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	spec := mock.QuotaSpec()

	restore, err := state.Restore()
	must.Nil(t, err)

	err = restore.QuotaSpecRestore(spec)
	must.Nil(t, err)
	restore.Commit()

	ws := memdb.NewWatchSet()
	out, err := state.QuotaSpecByName(ws, spec.Name)
	must.Nil(t, err)
	must.Eq(t, spec, out)
}

func TestStateStore_UpsertQuotaUsage(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	qs1 := mock.QuotaSpec()
	qs2 := mock.QuotaSpec()
	qu1 := mock.QuotaUsage()
	qu2 := mock.QuotaUsage()
	qu1.Name = qs1.Name
	qu2.Name = qs2.Name

	ws := memdb.NewWatchSet()
	out, err := state.QuotaUsageByName(ws, qu1.Name)
	must.Nil(t, out)
	must.Nil(t, err)
	out, err = state.QuotaUsageByName(ws, qu2.Name)
	must.Nil(t, out)
	must.Nil(t, err)

	must.Nil(t, state.UpsertQuotaSpecs(999, []*structs.QuotaSpec{qs1, qs2}))

	// Add a namespace and alloc using quota 1 to test "real" usage
	ns1 := mock.Namespace()
	ns1.Quota = qu1.Name
	must.Nil(t, state.UpsertNamespaces(998, []*structs.Namespace{ns1}))
	a1 := mock.Alloc()
	a1.Namespace = ns1.Name
	must.Nil(t, state.UpsertAllocs(structs.MsgTypeTestSetup, 1000, []*structs.Allocation{a1}))

	must.Nil(t, state.UpsertQuotaUsages(1001, []*structs.QuotaUsage{qu1, qu2}))
	must.True(t, watchFired(ws))

	ws = memdb.NewWatchSet()
	out, err = state.QuotaUsageByName(ws, qu1.Name)
	must.Nil(t, err)
	must.Eq(t, qu1.Name, out.Name)

	// CreateIndex should match when the *spec* was created
	must.Eq(t, uint64(999), out.CreateIndex, must.Sprintf("%d", out.CreateIndex))

	// ModifyIndex should match when the last upsert occurred
	must.Eq(t, uint64(1001), out.ModifyIndex, must.Sprintf("%d", qu1.ModifyIndex))

	// Compare to qu1 to ensure there's no accidental sharing of pointers
	must.NotEq(t, qu1.CreateIndex, out.CreateIndex)
	must.NotEq(t, qu1.ModifyIndex, out.ModifyIndex)

	for _, used := range out.Used {
		must.Eq(t, 500, used.RegionLimit.CPU)
		must.Eq(t, 256, used.RegionLimit.MemoryMB)
		must.Eq(t, 256, used.RegionLimit.MemoryMaxMB)
	}

	out, err = state.QuotaUsageByName(ws, qu2.Name)
	must.Nil(t, err)
	must.Eq(t, qu2.Name, out.Name)

	// Since upserting usages reconciles them, ensure quota 2's usage is 0
	for _, used := range out.Used {
		must.Zero(t, used.RegionLimit.CPU)
		must.Zero(t, used.RegionLimit.MemoryMB)
		must.Zero(t, used.RegionLimit.MemoryMaxMB)
	}

	iter, err := state.QuotaUsages(ws)
	must.Nil(t, err)

	// Ensure we see both usages
	count := 0
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		count++
	}
	must.Eq(t, 2, count)

	index, err := state.Index(TableQuotaUsage)
	must.Nil(t, err)
	must.Eq(t, 1001, index, must.Sprintf("%d", index))
	must.False(t, watchFired(ws))
}

func TestStateStore_DeleteQuotaUsages(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	qs1 := mock.QuotaSpec()
	qs2 := mock.QuotaSpec()
	qu1 := mock.QuotaUsage()
	qu2 := mock.QuotaUsage()
	qu1.Name = qs1.Name
	qu2.Name = qs2.Name

	// Create the quota usages
	must.Nil(t, state.UpsertQuotaSpecs(999, []*structs.QuotaSpec{qs1, qs2}))
	must.Nil(t, state.UpsertQuotaUsages(1000, []*structs.QuotaUsage{qu1, qu2}))

	// Create a watcher
	ws := memdb.NewWatchSet()
	_, err := state.QuotaUsageByName(ws, qu1.Name)
	must.Nil(t, err)

	// Delete the usage
	must.Nil(t, state.DeleteQuotaUsages(1001, []string{qu1.Name, qu2.Name}))

	// Ensure watching triggered
	must.True(t, watchFired(ws))

	// Ensure we don't get the object back
	ws = memdb.NewWatchSet()
	out, err := state.QuotaUsageByName(ws, qu1.Name)
	must.Nil(t, err)
	must.Nil(t, out)

	iter, err := state.QuotaUsages(ws)
	must.Nil(t, err)

	// Ensure we see both policies
	count := 0
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		count++
	}
	must.Zero(t, count)

	index, err := state.Index(TableQuotaUsage)
	must.Nil(t, err)
	must.Eq(t, 1001, index)
	must.False(t, watchFired(ws))
}

func TestStateStore_QuotaUsagesByNamePrefix(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	names := []string{
		"foo",
		"bar",
		"foobar",
		"foozip",
		"zip",
	}

	// Create the policies
	var baseIndex uint64 = 1000
	for _, name := range names {
		qs := mock.QuotaSpec()
		qs.Name = name
		qu := mock.QuotaUsage()
		qu.Name = name
		must.Nil(t, state.UpsertQuotaSpecs(baseIndex, []*structs.QuotaSpec{qs}))
		must.Nil(t, state.UpsertQuotaUsages(baseIndex+1, []*structs.QuotaUsage{qu}))
		baseIndex += 2
	}

	// Scan by prefix
	iter, err := state.QuotaUsagesByNamePrefix(nil, "foo")
	must.Nil(t, err)

	// Ensure we see both policies
	count := 0
	out := []string{}
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		count++
		out = append(out, raw.(*structs.QuotaUsage).Name)
	}
	must.Eq(t, 3, count)
	sort.Strings(out)

	expect := []string{"foo", "foobar", "foozip"}
	must.Eq(t, expect, out)
}

func TestStateStore_RestoreQuotaUsage(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	usage := mock.QuotaUsage()

	restore, err := state.Restore()
	must.Nil(t, err)

	err = restore.QuotaUsageRestore(usage)
	must.Nil(t, err)
	restore.Commit()

	ws := memdb.NewWatchSet()
	out, err := state.QuotaUsageByName(ws, usage.Name)
	must.Nil(t, err)
	must.Eq(t, usage, out)
}

func TestStateStore_UpsertLicense(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	stored, _ := mock.StoredLicense()

	must.Nil(t, state.UpsertLicense(1000, stored))

	ws := memdb.NewWatchSet()
	out, err := state.License(ws)
	must.NoError(t, err)
	must.Eq(t, out, stored)
}

func TestStateStore_UpsertTmpLicenseBarrier(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	stored := &structs.TmpLicenseBarrier{CreateTime: time.Now().UnixNano()}

	must.Nil(t, state.TmpLicenseSetBarrier(1000, stored))

	ws := memdb.NewWatchSet()
	out, err := state.TmpLicenseBarrier(ws)
	must.NoError(t, err)
	must.Eq(t, out, stored)
}

func TestStateStore_RestoreTmpLicenseBarrier(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)

	meta := &structs.TmpLicenseBarrier{CreateTime: time.Now().UnixNano()}

	restore, err := state.Restore()
	must.Nil(t, err)

	err = restore.TmpLicenseBarrierRestore(meta)
	must.Nil(t, err)
	restore.Commit()

	ws := memdb.NewWatchSet()
	out, err := state.TmpLicenseBarrier(ws)
	must.Nil(t, err)
	must.Eq(t, meta, out)
}

func TestStateStore_UpsertRecommendation(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	job := mock.Job()
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
	rec := mock.Recommendation(job)

	// Create a watchset so we can test that upsert fires the watch
	ws := memdb.NewWatchSet()
	_, err := state.RecommendationByID(ws, rec.ID)
	must.NoError(t, err)

	must.NoError(t, state.UpsertRecommendation(1000, rec))
	must.True(t, watchFired(ws))

	ws = memdb.NewWatchSet()
	out, err := state.RecommendationByID(ws, rec.ID)
	must.NoError(t, err)
	must.Eq(t, rec, out)

	index, err := state.Index(TableRecommendations)
	must.NoError(t, err)
	must.Eq(t, 1000, index)
	must.False(t, watchFired(ws))
}

func TestStateStore_ListRecommendationsByJob(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	job1 := mock.Job()
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job1))
	ns2 := mock.Namespace()
	must.NoError(t, state.UpsertNamespaces(909, []*structs.Namespace{ns2}))
	job2 := mock.Job()
	job2.Namespace = ns2.Name
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 910, nil, job2))

	// Create watchsets so we can test that upsert fires the watches
	wsList1 := memdb.NewWatchSet()
	_, err := state.RecommendationsByJob(wsList1, job1.Namespace, job1.ID, nil)
	must.NoError(t, err)
	wsList2 := memdb.NewWatchSet()
	_, err = state.RecommendationsByJob(wsList2, job2.Namespace, job2.ID, nil)
	must.NoError(t, err)

	rec1 := mock.Recommendation(job1)
	must.NoError(t, state.UpsertRecommendation(1000, rec1))
	must.True(t, watchFired(wsList1))

	rec2 := mock.Recommendation(job2)
	must.NoError(t, state.UpsertRecommendation(1001, rec2))
	must.True(t, watchFired(wsList2))

	wsList1 = memdb.NewWatchSet()
	out, err := state.RecommendationsByJob(wsList1, job1.Namespace, job1.ID, nil)
	must.NoError(t, err)
	must.Len(t, 1, out)
	must.Eq(t, rec1, out[0])

	wsList2 = memdb.NewWatchSet()
	out, err = state.RecommendationsByJob(wsList2, job2.Namespace, job2.ID, nil)
	must.NoError(t, err)
	must.Len(t, 1, out)
	must.Eq(t, rec2, out[0])

	index, err := state.Index(TableRecommendations)
	must.NoError(t, err)
	must.Eq(t, 1001, index)
}

func TestStateStore_ListRecommendationsByNamespace(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	job1 := mock.Job()
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job1))
	ns2 := mock.Namespace()
	must.NoError(t, state.UpsertNamespaces(909, []*structs.Namespace{ns2}))
	job2 := mock.Job()
	job2.Namespace = ns2.Name
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 910, nil, job2))
	job3 := mock.Job()
	job3.Namespace = ns2.Name
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 915, nil, job3))

	// Create watchsets so we can test that upsert fires the watches
	wsList1 := memdb.NewWatchSet()
	_, err := state.RecommendationsByNamespace(wsList1, job1.Namespace)
	must.NoError(t, err)
	wsList2 := memdb.NewWatchSet()
	_, err = state.RecommendationsByNamespace(wsList2, job2.Namespace)
	must.NoError(t, err)

	rec1 := mock.Recommendation(job1)
	must.NoError(t, state.UpsertRecommendation(1000, rec1))
	must.True(t, watchFired(wsList1))
	rec2 := mock.Recommendation(job2)
	must.NoError(t, state.UpsertRecommendation(1001, rec2))
	must.True(t, watchFired(wsList2))
	rec3 := mock.Recommendation(job3)
	must.NoError(t, state.UpsertRecommendation(1002, rec3))

	wsList1 = memdb.NewWatchSet()
	out, err := state.RecommendationsByNamespace(wsList1, job1.Namespace)
	must.NoError(t, err)
	must.Len(t, 1, out)
	must.Eq(t, rec1, out[0])

	wsList2 = memdb.NewWatchSet()
	out, err = state.RecommendationsByNamespace(wsList2, job2.Namespace)
	must.NoError(t, err)
	must.Len(t, 2, out)
	outIds := []string{out[0].ID, out[1].ID}
	expIds := []string{rec2.ID, rec3.ID}
	sort.Strings(outIds)
	sort.Strings(expIds)
	must.Eq(t, expIds, outIds)

	index, err := state.Index(TableRecommendations)
	must.NoError(t, err)
	must.Eq(t, 1002, index)
}

func TestStateStore_ListAllRecommendations(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	job1 := mock.Job()
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job1))
	ns2 := mock.Namespace()
	must.NoError(t, state.UpsertNamespaces(909, []*structs.Namespace{ns2}))
	job2 := mock.Job()
	job2.Namespace = ns2.Name
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 910, nil, job2))
	job3 := mock.Job()
	job3.Namespace = ns2.Name
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 915, nil, job3))

	// Create watchsets so we can test that upsert fires the watches
	wsList := memdb.NewWatchSet()
	_, err := state.Recommendations(wsList)
	must.NoError(t, err)

	rec1 := mock.Recommendation(job1)
	must.NoError(t, state.UpsertRecommendation(1000, rec1))
	must.True(t, watchFired(wsList))
	rec2 := mock.Recommendation(job2)
	must.NoError(t, state.UpsertRecommendation(1001, rec2))
	rec3 := mock.Recommendation(job3)
	must.NoError(t, state.UpsertRecommendation(1002, rec3))

	wsList = memdb.NewWatchSet()
	out, err := state.Recommendations(wsList)
	outRecs := []*structs.Recommendation{}
	for {
		raw := out.Next()
		if raw == nil {
			break
		}
		outRecs = append(outRecs, raw.(*structs.Recommendation))
	}
	must.NoError(t, err)
	must.Len(t, 3, outRecs)
	outIds := []string{outRecs[0].ID, outRecs[1].ID, outRecs[2].ID}
	expIds := []string{rec1.ID, rec2.ID, rec3.ID}
	sort.Strings(outIds)
	sort.Strings(expIds)
	must.Eq(t, expIds, outIds)

	index, err := state.Index(TableRecommendations)
	must.NoError(t, err)
	must.Eq(t, 1002, index)
}

// upserting a recommendation with the same job,path will update
// any existing recommendation with that job,path
func TestStateStore_UpsertRecommendation_UpdateExistingPath(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	job := mock.Job()
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
	job.TaskGroups[0].Name = "this is a [more interesting] group name 🤣"
	job.TaskGroups[0].Tasks[0].Name = "and this is a [more interesting] task name 🤣🤣🤣"

	rec := mock.Recommendation(job)
	must.NoError(t, state.UpsertRecommendation(1000, rec))

	wsOrig := memdb.NewWatchSet()
	out, err := state.RecommendationByID(wsOrig, rec.ID)
	must.NoError(t, err)
	must.Eq(t, rec, out)

	wsList := memdb.NewWatchSet()
	list, err := state.RecommendationsByJob(wsList, job.Namespace, job.ID, nil)
	must.NoError(t, err)
	must.Len(t, 1, list)

	updatedRec := rec.Copy()
	updatedRec.Value = 750
	updatedRec.ID = uuid.Generate() // this should be overwritten on the Path match
	updatedRec.Meta["updated"] = true
	must.NoError(t, state.UpsertRecommendation(1010, updatedRec))

	must.True(t, watchFired(wsOrig))
	must.True(t, watchFired(wsList))

	list, err = state.RecommendationsByJob(wsList, job.Namespace, job.ID, nil)
	must.NoError(t, err)
	must.Len(t, 1, list)
	must.Eq(t, rec.ID, list[0].ID)
	must.Eq(t, updatedRec.Value, list[0].Value)
	must.True(t, list[0].Meta["updated"].(bool))

	index, err := state.Index(TableRecommendations)
	must.NoError(t, err)
	must.GreaterEq(t, index, uint64(1010))
}

func TestStateStore_UpsertRecommendation_ErrorWithoutJob(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	job := mock.Job()
	rec := mock.Recommendation(job)

	// Create a watchset so we can test that delete fires the watch
	ws := memdb.NewWatchSet()
	_, err := state.RecommendationByID(ws, rec.ID)
	must.NoError(t, err)

	prevIndex, err := state.Index(TableRecommendations)
	must.NoError(t, err)

	must.Error(t, state.UpsertRecommendation(1000, rec), must.Sprint("job does not exist"))

	out, err := state.RecommendationByID(nil, rec.ID)
	must.NoError(t, err)
	must.Nil(t, out)

	index, err := state.Index(TableRecommendations)
	must.NoError(t, err)
	must.Eq(t, prevIndex, index)
	must.False(t, watchFired(ws))
}

func TestStateStore_DeleteRecommendation(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	job := mock.Job()
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
	rec1 := mock.Recommendation(job)
	rec2 := mock.Recommendation(job)
	rec2.Target(job.TaskGroups[0].Name, job.TaskGroups[0].Tasks[0].Name, "MemoryMB")
	rec2.Value = 500

	must.NoError(t, state.UpsertRecommendation(1000, rec1))
	must.NoError(t, state.UpsertRecommendation(1010, rec2))
	// Create a watchset so we can test that delete fires the watch
	ws := memdb.NewWatchSet()
	list, err := state.RecommendationsByJob(ws, job.Namespace, job.ID, nil)
	must.NoError(t, err)
	must.Len(t, 2, list)

	must.NoError(t, state.DeleteRecommendations(1020, []string{rec1.ID, rec2.ID}))
	must.True(t, watchFired(ws))

	ws = memdb.NewWatchSet()
	out, err := state.RecommendationByID(ws, rec1.ID)
	must.NoError(t, err)
	must.Nil(t, out)

	out, err = state.RecommendationByID(ws, rec2.ID)
	must.NoError(t, err)
	must.Nil(t, out)

	index, err := state.Index(TableRecommendations)
	must.NoError(t, err)
	must.Eq(t, 1020, index)
	must.False(t, watchFired(ws))
}

func TestStateStore_DeleteJob_DeletesRecommendations(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	job := mock.Job()
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
	rec1 := mock.Recommendation(job)
	rec2 := mock.Recommendation(job)
	rec2.Target(job.TaskGroups[0].Name, job.TaskGroups[0].Tasks[0].Name, "MemoryMB")
	rec2.Value = 500

	must.NoError(t, state.UpsertRecommendation(1000, rec1))
	must.NoError(t, state.UpsertRecommendation(1010, rec2))
	// Create a watchset so we can test that delete fires the watch
	ws := memdb.NewWatchSet()
	_, err := state.RecommendationByID(ws, rec1.ID)
	must.NoError(t, err)

	must.Nil(t, state.DeleteJob(1020, job.Namespace, job.ID))

	out, err := state.RecommendationByID(nil, rec1.ID)
	must.NoError(t, err)
	must.Nil(t, out)

	out, err = state.RecommendationByID(nil, rec2.ID)
	must.NoError(t, err)
	must.Nil(t, out)

	index, err := state.Index(TableRecommendations)
	must.NoError(t, err)
	must.GreaterEq(t, index, uint64(1020))
	must.True(t, watchFired(ws))
}

func TestStateStore_UpdateJob_DeletesFixedRecommendations(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	job := mock.Job()
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
	rec1 := mock.Recommendation(job)
	rec1.EnforceVersion = true
	rec2 := mock.Recommendation(job)
	rec2.Target(job.TaskGroups[0].Name, job.TaskGroups[0].Tasks[0].Name, "MemoryMB")
	rec2.Value = 500
	rec2.Current = job.TaskGroups[0].Tasks[0].Resources.MemoryMB
	rec2.EnforceVersion = false

	must.NoError(t, state.UpsertRecommendation(1000, rec1))
	must.NoError(t, state.UpsertRecommendation(1010, rec2))
	// Create watchsets so we can test that update fires appropriately
	ws1 := memdb.NewWatchSet()
	_, err := state.RecommendationByID(ws1, rec1.ID)
	must.NoError(t, err)
	ws2 := memdb.NewWatchSet()
	_, err = state.RecommendationByID(ws2, rec2.ID)
	must.NoError(t, err)

	updatedJob := job.Copy()
	updatedJob.Meta["updated"] = "true"
	updatedJob.Version = job.Version + 1
	must.Nil(t, state.UpsertJob(structs.MsgTypeTestSetup, 1020, nil, updatedJob))

	out, err := state.RecommendationByID(nil, rec1.ID)
	must.NoError(t, err)
	must.Nil(t, out)
	must.True(t, watchFired(ws1))

	out, err = state.RecommendationByID(nil, rec2.ID)
	must.NoError(t, err)
	must.NotNil(t, out)
	must.False(t, watchFired(ws2))

	index, err := state.Index(TableRecommendations)
	must.NoError(t, err)
	must.GreaterEq(t, index, uint64(1020))
}

// TestStateStore_UpdateJob_DeletesOrphanedRecommendations_Group tests that
// recommendations against a specific task are automatically deleted if the
// task is removed from the job
func TestStateStore_UpdateJob_DeletesOrphanedRecommendations_DeleteGroup(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	job := mock.Job()
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
	rec := mock.Recommendation(job)

	must.NoError(t, state.UpsertRecommendation(1000, rec))
	// Create watchsets so we can test that update fires appropriately
	ws := memdb.NewWatchSet()
	_, err := state.RecommendationByID(ws, rec.ID)
	must.NoError(t, err)

	updatedJob := job.Copy()
	updatedJob.TaskGroups[0].Name = "new task group"
	updatedJob.Version = job.Version + 1
	must.Nil(t, state.UpsertJob(structs.MsgTypeTestSetup, 1020, nil, updatedJob))

	out, err := state.RecommendationByID(nil, rec.ID)
	must.NoError(t, err)
	must.Nil(t, out)
	must.True(t, watchFired(ws))

	index, err := state.Index(TableRecommendations)
	must.NoError(t, err)
	must.GreaterEq(t, index, uint64(1020))
}

// TestStateStore_UpdateJob_DeletesOrphanedRecommendations_Task tests that
// recommendations against a specific task are automatically deleted if the
// task is removed from the job
func TestStateStore_UpdateJob_DeletesOrphanedRecommendations_DeleteTask(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	job := mock.Job()
	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
	rec := mock.Recommendation(job)

	must.NoError(t, state.UpsertRecommendation(1000, rec))
	// Create watchsets so we can test that update fires appropriately
	ws := memdb.NewWatchSet()
	_, err := state.RecommendationByID(ws, rec.ID)
	must.NoError(t, err)

	updatedJob := job.Copy()
	updatedJob.TaskGroups[0].Tasks[0].Name = "new task"
	updatedJob.Version = job.Version + 1
	must.Nil(t, state.UpsertJob(structs.MsgTypeTestSetup, 1020, nil, updatedJob))

	out, err := state.RecommendationByID(nil, rec.ID)
	must.NoError(t, err)
	must.Nil(t, out)
	must.True(t, watchFired(ws))

	index, err := state.Index(TableRecommendations)
	must.NoError(t, err)
	must.GreaterEq(t, index, uint64(1020))
}

// TestStateStore_UpdateJob_UpdateRecCurrent tests that recommendations against a
// job have .Current updated if the job is updated
func TestStateStore_UpdateJob_UpdateRecCurrent(t *testing.T) {
	ci.Parallel(t)
	state := testStateStore(t)
	job := mock.Job()

	must.NoError(t, state.UpsertJob(structs.MsgTypeTestSetup, 900, nil, job))
	recCPU := mock.Recommendation(job)
	recMem := mock.Recommendation(job)
	recMem.Resource = "MemoryMB"
	recMem.Current = job.TaskGroups[0].Tasks[0].Resources.MemoryMB
	recMem.Value = recMem.Current * 2

	must.NoError(t, state.UpsertRecommendation(1000, recCPU))
	must.NoError(t, state.UpsertRecommendation(1000, recMem))
	// Create watchsets so we can test that update fires appropriately
	wsCPU := memdb.NewWatchSet()
	_, err := state.RecommendationByID(wsCPU, recCPU.ID)
	must.NoError(t, err)
	wsMem := memdb.NewWatchSet()
	_, err = state.RecommendationByID(wsMem, recMem.ID)
	must.NoError(t, err)

	updatedJob := job.Copy()
	updatedJob.TaskGroups[0].Tasks[0].Resources.CPU *= 4
	updatedJob.TaskGroups[0].Tasks[0].Resources.MemoryMB *= 4
	updatedJob.Version = job.Version + 1
	must.Nil(t, state.UpsertJob(structs.MsgTypeTestSetup, 1020, nil, updatedJob))

	must.True(t, watchFired(wsCPU))
	out, err := state.RecommendationByID(nil, recCPU.ID)
	must.NoError(t, err)
	must.NotNil(t, out)
	must.Eq(t, updatedJob.TaskGroups[0].Tasks[0].Resources.CPU, out.Current)

	must.True(t, watchFired(wsMem))
	out, err = state.RecommendationByID(nil, recMem.ID)
	must.NoError(t, err)
	must.NotNil(t, out)
	must.Eq(t, updatedJob.TaskGroups[0].Tasks[0].Resources.MemoryMB, out.Current)

	index, err := state.Index(TableRecommendations)
	must.NoError(t, err)
	must.GreaterEq(t, index, uint64(1020))
}

func TestStateStore_ScalingPoliciesByType_Vertical(t *testing.T) {
	ci.Parallel(t)

	state := testStateStore(t)

	// Create scaling policies of different types
	pVertMem := mock.ScalingPolicy()
	pVertMem.Type = structs.ScalingPolicyTypeVerticalMem

	pVertCPU := mock.ScalingPolicy()
	pVertCPU.Type = structs.ScalingPolicyTypeVerticalCPU

	// Create search routine
	search := func(t string) (found []string, err error) {
		found = []string{}
		iter, err := state.ScalingPoliciesByTypePrefix(nil, t)
		if err != nil {
			return
		}

		for raw := iter.Next(); raw != nil; raw = iter.Next() {
			found = append(found, raw.(*structs.ScalingPolicy).Type)
		}
		return
	}

	// Create the policies
	var baseIndex uint64 = 1000
	err := state.UpsertScalingPolicies(baseIndex, []*structs.ScalingPolicy{pVertCPU, pVertMem})
	must.NoError(t, err)

	// Check if we can read vertical_cpu policies
	actual, err := search(structs.ScalingPolicyTypeVerticalCPU)
	must.NoError(t, err)
	expect := []string{pVertCPU.Type}
	must.SliceContainsAll(t, expect, actual)

	// Check if we can read vertical_mem policies
	actual, err = search(structs.ScalingPolicyTypeVerticalMem)
	must.NoError(t, err)
	expect = []string{pVertMem.Type}
	must.SliceContainsAll(t, expect, actual)

	// Check if we can read vertical prefix policies
	expect = []string{pVertCPU.Type, pVertMem.Type}
	actual, err = search("vertical")
	must.NoError(t, err)
	must.SliceContainsAll(t, expect, actual)
}

func TestStateStore_ScalingPoliciesByTypePrefix_Vertical(t *testing.T) {
	ci.Parallel(t)

	state := testStateStore(t)

	// Create scaling policies of different types
	pVertMem := mock.ScalingPolicy()
	pVertMem.Type = structs.ScalingPolicyTypeVerticalMem

	pVertCPU := mock.ScalingPolicy()
	pVertCPU.Type = structs.ScalingPolicyTypeVerticalCPU

	// Create search routine
	search := func(t string) (count int, found []string, err error) {
		found = []string{}
		iter, err := state.ScalingPoliciesByTypePrefix(nil, t)
		if err != nil {
			return
		}

		for raw := iter.Next(); raw != nil; raw = iter.Next() {
			count++
			found = append(found, raw.(*structs.ScalingPolicy).Type)
		}
		return
	}

	// Create the policies
	var baseIndex uint64 = 1000
	err := state.UpsertScalingPolicies(baseIndex, []*structs.ScalingPolicy{pVertCPU, pVertMem})
	must.NoError(t, err)

	// Check if we can read vertical prefix policies
	expect := []string{pVertCPU.Type, pVertMem.Type}
	count, found, err := search("vertical")

	sort.Strings(found)
	sort.Strings(expect)

	must.NoError(t, err)
	must.Eq(t, expect, found)
	must.Eq(t, 2, count)
}

func TestStateStore_UpsertJob_UpsertScalingPolicies(t *testing.T) {
	ci.Parallel(t)

	state := testStateStore(t)
	job, policy := mock.JobWithScalingPolicy()

	// Create a watchset so we can test that upsert fires the watch
	ws := memdb.NewWatchSet()
	out, err := state.ScalingPolicyByTargetAndType(ws, policy.Target, structs.ScalingPolicyTypeHorizontal)
	must.NoError(t, err)
	must.Nil(t, out)

	var newIndex uint64 = 1000
	err = state.UpsertJob(structs.MsgTypeTestSetup, newIndex, nil, job)
	must.NoError(t, err)
	must.True(t, watchFired(ws), must.Sprint("watch should have fired on job upsert"))

	out, err = state.ScalingPolicyByTargetAndType(nil, policy.Target, policy.Type)
	must.NoError(t, err)
	must.NotNil(t, out)
	must.Eq(t, newIndex, out.CreateIndex)
	must.Eq(t, newIndex, out.ModifyIndex)

	index, err := state.Index("scaling_policy")
	must.NoError(t, err)
	must.Eq(t, newIndex, index)

	cpuPolicy := &structs.ScalingPolicy{
		ID:      uuid.Generate(),
		Type:    structs.ScalingPolicyTypeVerticalCPU,
		Policy:  map[string]interface{}{},
		Min:     100,
		Max:     1000,
		Enabled: true,
	}
	job.TaskGroups[0].Tasks[0].ScalingPolicies = []*structs.ScalingPolicy{cpuPolicy}
	cpuPolicy.TargetTask(job, job.TaskGroups[0], job.TaskGroups[0].Tasks[0])

	out, err = state.ScalingPolicyByTargetAndType(nil, policy.Target, policy.Type)
	newIndex = 1100
	err = state.UpsertJob(structs.MsgTypeTestSetup, newIndex, nil, job)
	must.NoError(t, err)
	must.True(t, watchFired(ws), must.Sprint("watch should have fired on job upsert"))

	// old policy is undisturbed
	out, err = state.ScalingPolicyByTargetAndType(nil, policy.Target, policy.Type)
	must.NoError(t, err)
	must.NotNil(t, out)
	must.Eq(t, 1000, out.CreateIndex)
	must.Eq(t, 1000, out.ModifyIndex)

	// new policy is created
	out, err = state.ScalingPolicyByTargetAndType(nil, cpuPolicy.Target, cpuPolicy.Type)
	must.NoError(t, err)
	must.NotNil(t, out)
	must.Eq(t, newIndex, out.CreateIndex)
	must.Eq(t, newIndex, out.ModifyIndex)

	index, err = state.Index("scaling_policy")
	must.NoError(t, err)
	must.Eq(t, newIndex, index, must.Sprint("table index should be updated"))

	// simple change on job
	job.Meta["updated"] = "yes"
	ws = memdb.NewWatchSet()
	out, err = state.ScalingPolicyByTargetAndType(ws, policy.Target, policy.Type)
	newIndex = 1200
	err = state.UpsertJob(structs.MsgTypeTestSetup, newIndex, nil, job)
	must.NoError(t, err)
	must.False(t, watchFired(ws), must.Sprint("watch should not have fired on job upsert"))
	index, err = state.Index("scaling_policy")
	must.NoError(t, err)
	must.Eq(t, 1100, index, must.Sprint("table index should not be updated"))
}
