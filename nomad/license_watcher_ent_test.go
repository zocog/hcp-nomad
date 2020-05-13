// build +ent

package nomad

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"

	"github.com/stretchr/testify/require"
)

func newTestLicenseWatcher() *LicenseWatcher {
	logger := hclog.NewInterceptLogger(nil)
	lw, _ := NewLicenseWatcher(logger)
	return lw
}

func testShutdownFunc() error {
	return nil
}

func TestLicenseWatcher_UpdatingWatcher(t *testing.T) {
	t.Parallel()
	TestLicenseValidationHelper(t)

	ctx := context.Background()
	lw := newTestLicenseWatcher()
	state := state.TestStateStore(t)
	lw.start(ctx, state, testShutdownFunc)
	initLicense, _ := lw.watcher.License()
	newLicense := license.NewTestLicense(license.TestGovernancePolicyFlags())
	stored := &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1000),
	}
	state.UpsertLicense(1000, stored)
	time.Sleep(1 * time.Second)
	fetchedLicense, err := lw.watcher.License()
	require.NoError(t, err)
	require.False(t, fetchedLicense.Equal(initLicense), "fetched license should be different from the inital")
	require.True(t, fetchedLicense.Equal(newLicense.License.License), fmt.Sprintf("got: %s wanted: %s", fetchedLicense, newLicense.License.License))
}

func TestLicenseWatcher_UpdateCh(t *testing.T) {
	t.Parallel()
	TestLicenseValidationHelper(t)

	lw := newTestLicenseWatcher()
	state := state.TestStateStore(t)
	lw.start(context.Background(), state, testShutdownFunc)
	newLicense := license.NewTestLicense(license.TestGovernancePolicyFlags())
	stored := &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1000),
	}
	state.UpsertLicense(1000, stored)
	time.Sleep(1 * time.Second)

	require.NotEqual(t, lw.features, uint64(0))
	require.Equal(t, lw.license.Features, license.Features(lw.features))
	require.True(t, lw.HasFeature(license.FeatureAuditLogging))
}

func TestLicenseWatcher_UpdateCh_Platform(t *testing.T) {
	t.Parallel()
	TestLicenseValidationHelper(t)

	lw := newTestLicenseWatcher()
	state := state.TestStateStore(t)
	lw.start(context.Background(), state, testShutdownFunc)
	newLicense := license.NewTestLicense(license.TestPlatformFlags())
	stored := &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1000),
	}
	state.UpsertLicense(1000, stored)
	time.Sleep(1 * time.Second)

	require.NotEqual(t, lw.features, uint64(0))
	require.Equal(t, lw.license.Features, license.Features(lw.features))
	require.False(t, lw.HasFeature(license.FeatureAuditLogging))
	require.True(t, lw.HasFeature(license.FeatureReadScalability))
}

func TestLicenseWatcher_FeatureCheck(t *testing.T) {
	cases := []struct {
		desc            string
		licenseFeatures license.Features
		f               license.Features
		has             bool
	}{
		{
			desc:            "contains feature",
			licenseFeatures: license.FeatureAuditLogging | license.FeatureNamespaces,
			f:               license.FeatureAuditLogging,
			has:             true,
		},
		{
			desc:            "missing feature",
			licenseFeatures: license.FeatureAuditLogging | license.FeatureNamespaces,
			f:               license.FeatureAutoUpgrades,
			has:             false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			lw := newTestLicenseWatcher()
			atomic.StoreUint64(&lw.features, uint64(tc.licenseFeatures))
			require.Equal(t, tc.has, lw.HasFeature(tc.f))
		})
	}
}

func TestLicenseWatcher_PeriodicLogging(t *testing.T) {
	lw := newTestLicenseWatcher()
	atomic.StoreUint64(&lw.features, uint64(0))

	require.Error(t, lw.FeatureCheck(license.FeatureAuditLogging))
	require.Len(t, lw.logTimes, 1)
	t1 := lw.logTimes[fmt.Sprintf("Feature %q is unlicensed", license.FeatureAuditLogging)]
	require.NotNil(t, t1)

	// Fire another feature check
	require.Error(t, lw.FeatureCheck(license.FeatureAuditLogging))

	t2 := lw.logTimes[fmt.Sprintf("Feature %q is unlicensed", license.FeatureAuditLogging)]
	require.NotNil(t, t2)

	require.Equal(t, t1, t2)
}
