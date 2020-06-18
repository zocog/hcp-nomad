// build +ent

package nomad

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-licensing/license"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"

	"github.com/stretchr/testify/require"
)

func newTestLicenseWatcher() *LicenseWatcher {
	logger := hclog.NewInterceptLogger(nil)
	cfg := &LicenseConfig{
		AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
	}

	lw, _ := NewLicenseWatcher(logger, cfg)
	return lw
}

func testShutdownFunc() error {
	return nil
}

func previousID(t *testing.T, lw *LicenseWatcher) string {
	lic, err := lw.GetLicense()
	require.NoError(t, err)
	return lic.LicenseID
}

func TestLicenseWatcher_UpdatingWatcher(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	lw := newTestLicenseWatcher()
	state := state.TestStateStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	lw.start(ctx, state, testShutdownFunc)
	defer cancel()

	initLicense, _ := lw.watcher.License()
	newLicense := license.NewTestLicense(license.TestGovernancePolicyFlags())
	stored := &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1000),
	}
	previousID := previousID(t, lw)
	state.UpsertLicense(1000, stored)
	waitForLicense(t, lw, previousID)

	fetchedLicense, err := lw.watcher.License()

	require.NoError(t, err)
	require.False(t, fetchedLicense.Equal(initLicense), "fetched license should be different from the inital")
	require.True(t, fetchedLicense.Equal(newLicense.License.License), fmt.Sprintf("got: %s wanted: %s", fetchedLicense, newLicense.License.License))
}

func TestLicenseWatcher_UpdateCh(t *testing.T) {
	t.Parallel()

	lw := newTestLicenseWatcher()
	state := state.TestStateStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	lw.start(ctx, state, testShutdownFunc)
	defer cancel()

	newLicense := license.NewTestLicense(temporaryFlags())
	stored := &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1000),
	}
	previousID := previousID(t, lw)
	state.UpsertLicense(1000, stored)
	waitForLicense(t, lw, previousID)

	require.NotEqual(t, lw.features, uint64(0))
	require.True(t, lw.HasFeature(license.FeatureAuditLogging))
	require.True(t, lw.HasFeature(license.FeatureMultiregionDeployments))
}

func TestLicenseWatcher_Validate(t *testing.T) {
	t.Parallel()

	lw := newTestLicenseWatcher()

	state := state.TestStateStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	lw.start(ctx, state, testShutdownFunc)
	defer cancel()

	invalidFlags := map[string]interface{}{
		"modules": []interface{}{"invalid"},
	}
	newLicense := license.NewTestLicense(invalidFlags)

	// It can be a valid go-licensing license
	_, err := lw.watcher.ValidateLicense(newLicense.Signed)
	require.NoError(t, err)

	// Ensure it is not a valid nomad license
	lic, err := lw.ValidateLicense(newLicense.Signed)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid module")
	require.Nil(t, lic)
}

func TestLicenseWatcher_UpdateCh_Platform(t *testing.T) {
	t.Parallel()

	lw := newTestLicenseWatcher()
	state := state.TestStateStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	lw.start(ctx, state, testShutdownFunc)
	defer cancel()

	newLicense := license.NewTestLicense(license.TestPlatformFlags())
	stored := &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1000),
	}
	previousID := previousID(t, lw)

	state.UpsertLicense(1000, stored)
	waitForLicense(t, lw, previousID)

	require.NotEqual(t, lw.features, uint64(0))
	require.False(t, lw.HasFeature(license.FeatureAuditLogging))
	require.True(t, lw.HasFeature(license.FeatureReadScalability))
}

func waitForLicense(t *testing.T, lw *LicenseWatcher, previousID string) {
	testutil.WaitForResult(func() (bool, error) {
		l, err := lw.GetLicense()
		require.NoError(t, err)
		if l.LicenseID == previousID {
			return false, fmt.Errorf("expected updated license")
		}
		return true, nil
	}, func(err error) {
		require.FailNow(t, err.Error())
	})
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

	require.Error(t, lw.FeatureCheck(license.FeatureAuditLogging, true))
	require.Len(t, lw.logTimes, 1)
	t1 := lw.logTimes[license.FeatureAuditLogging]
	require.NotNil(t, t1)

	// Fire another feature check
	require.Error(t, lw.FeatureCheck(license.FeatureAuditLogging, true))

	t2 := lw.logTimes[license.FeatureAuditLogging]
	require.NotNil(t, t2)

	require.Equal(t, t1, t2)
}
