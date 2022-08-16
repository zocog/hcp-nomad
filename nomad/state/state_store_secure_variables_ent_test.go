package state

import (
	"fmt"
	"math"
	"testing"

	"github.com/shoenig/test/must"

	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
)

func TestStateStore_SecureVariables_QuotaEnforcement(t *testing.T) {
	ci.Parallel(t)
	store := testStateStore(t)

	index := uint64(20)

	spec := mock.QuotaSpec()
	spec.Limits[0].SecureVariablesLimit = 1  // region: global
	spec.Limits[1].SecureVariablesLimit = 20 // region: europe
	spec.SetHash()
	index++
	must.NoError(t, store.UpsertQuotaSpecs(index, []*structs.QuotaSpec{spec}))

	ns := mock.Namespace()
	otherNs := mock.Namespace()
	ns.Quota = spec.Name
	otherNs.Quota = spec.Name
	must.NoError(t, store.UpsertNamespaces(index, []*structs.Namespace{ns, otherNs}))

	// fits in quota
	var1 := mock.SecureVariableEncrypted()
	var1.Path = "var1"
	var1.Namespace = ns.Name
	var1.Data = []byte("123456789")
	index++
	resp := store.SVESet(index, &structs.SVApplyStateRequest{
		Op:  structs.SVOpSet,
		Var: var1,
	})
	must.NoError(t, resp.Error)

	// doesn't fit inside quota
	var2 := mock.SecureVariableEncrypted()
	var2.Path = "var2"
	var2.Namespace = ns.Name
	var2.Data = make([]byte, structs.BytesInMegabyte)
	index++
	resp = store.SVESet(index, &structs.SVApplyStateRequest{
		Op:  structs.SVOpSet,
		Var: var2,
	})
	must.Error(t, resp.Error)

	// no quota tracking update if quota is enforced
	quotaUsed, err := store.SecureVariablesQuotaByNamespace(nil, ns.Name)
	must.NoError(t, err)
	must.Eq(t, quotaUsed.Size, 9)

	// modifying existing doesn't change quota usage, so no enforcement happens
	var1.Data = []byte("12345678x")
	index++
	resp = store.SVESet(index, &structs.SVApplyStateRequest{
		Op:  structs.SVOpSet,
		Var: var1,
	})
	must.NoError(t, resp.Error)

	// quotas are shared by all namespaces in a region assigned that quota
	var2.Namespace = otherNs.Name
	index++
	resp = store.SVESet(index, &structs.SVApplyStateRequest{
		Op:  structs.SVOpSet,
		Var: var2,
	})
	must.EqError(t, resp.Error,
		fmt.Errorf("quota %q exceeded for namespace %q", spec.Name, otherNs.Name).Error())

}

func TestBytesToMegaBytes(t *testing.T) {
	ci.Parallel(t)

	testCases := []struct {
		inBytes     int64
		inMegaBytes int
	}{
		{inBytes: 0, inMegaBytes: 0},
		{inBytes: -1, inMegaBytes: 0},
		{inBytes: structs.BytesInMegabyte, inMegaBytes: 1},
		{inBytes: structs.BytesInMegabyte + 1, inMegaBytes: 2},
		{inBytes: math.MaxInt64, inMegaBytes: 8796093022208},
	}
	for _, tc := range testCases {
		tc := tc
		must.Eq(t, bytesToMegaBytes(tc.inBytes), tc.inMegaBytes)
	}
}
