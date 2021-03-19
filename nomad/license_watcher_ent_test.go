// +build ent

package nomad

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/go-licensing"
	"github.com/hashicorp/nomad-licensing/license"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"

	"github.com/stretchr/testify/require"
)

func previousID(t *testing.T, lw *LicenseWatcher) string {
	return lw.License().LicenseID
}

func waitForLicense(t *testing.T, lw *LicenseWatcher, previousID string) {
	testutil.WaitForResult(func() (bool, error) {
		l := lw.License()
		if l.LicenseID == previousID {
			return false, fmt.Errorf("expected updated license")
		}
		return true, nil
	}, func(err error) {
		require.FailNow(t, err.Error())
	})
}

func TestLicenseWatcher_UpdatingWatcher(t *testing.T) {
	t.Parallel()

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanupS1()
	state := s1.State()
	lw := s1.EnterpriseState.licenseWatcher

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

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanupS1()
	state := s1.State()
	lw := s1.EnterpriseState.licenseWatcher

	newLicense := license.NewTestLicense(temporaryFlags())
	stored := &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1000),
	}
	previousID := previousID(t, lw)
	state.UpsertLicense(1000, stored)
	waitForLicense(t, lw, previousID)

	require.NotEqual(t, uint64(lw.License().Features), uint64(0))
	require.True(t, lw.hasFeature(license.FeatureAuditLogging))
	require.True(t, lw.hasFeature(license.FeatureMultiregionDeployments))
}

func TestLicenseWatcher_Validate(t *testing.T) {
	t.Parallel()

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanupS1()
	lw := s1.EnterpriseState.licenseWatcher

	invalidFlags := map[string]interface{}{
		"modules": []interface{}{"invalid"},
	}
	newLicense := license.NewTestLicense(invalidFlags)

	// Ensure it is not a valid nomad license
	lic, err := lw.ValidateLicense(newLicense.Signed)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid module")
	require.Nil(t, lic)
}

func TestLicenseWatcher_UpdateCh_Platform(t *testing.T) {
	t.Parallel()

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanupS1()
	state := s1.State()
	lw := s1.EnterpriseState.licenseWatcher

	newLicense := license.NewTestLicense(license.TestPlatformFlags())
	stored := &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1000),
	}
	previousID := previousID(t, lw)

	state.UpsertLicense(1000, stored)
	waitForLicense(t, lw, previousID)

	require.NotEqual(t, uint64(lw.License().Features), uint64(0))
	require.False(t, lw.hasFeature(license.FeatureAuditLogging))
	require.True(t, lw.hasFeature(license.FeatureReadScalability))
}

func TestLicenseWatcher_FeatureCheck(t *testing.T) {
	cases := []struct {
		desc            string
		licenseFeatures []string
		f               license.Features
		has             bool
	}{
		{
			desc:            "contains feature",
			licenseFeatures: []string{license.FeatureAuditLogging.String(), license.FeatureNamespaces.String()},
			f:               license.FeatureAuditLogging,
			has:             true,
		},
		{
			desc:            "missing feature",
			licenseFeatures: []string{license.FeatureAuditLogging.String(), license.FeatureNamespaces.String()},
			f:               license.FeatureMultiregionDeployments,
			has:             false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			// start watcher
			s1, cleanupS1 := TestServer(t, func(c *Config) {
				c.LicenseConfig = &LicenseConfig{
					AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
				}
			})
			defer cleanupS1()
			state := s1.State()
			lw := s1.EnterpriseState.licenseWatcher

			flags := map[string]interface{}{
				"features": map[string]interface{}{
					"add": tc.licenseFeatures,
				},
			}

			previousID := previousID(t, lw)
			newLicense := license.NewTestLicense(flags)

			stored := &structs.StoredLicense{
				Signed:      newLicense.Signed,
				CreateIndex: uint64(1000),
			}
			state.UpsertLicense(1000, stored)
			waitForLicense(t, lw, previousID)

			require.Equal(t, tc.has, lw.hasFeature(tc.f))
		})
	}
}

func TestLicenseWatcher_PeriodicLogging(t *testing.T) {
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanupS1()
	state := s1.State()
	lw := s1.EnterpriseState.licenseWatcher

	// Create license without any added features
	flags := map[string]interface{}{}

	previousID := previousID(t, s1.EnterpriseState.licenseWatcher)
	newLicense := license.NewTestLicense(flags)

	stored := &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1000),
	}
	state.UpsertLicense(1000, stored)
	waitForLicense(t, lw, previousID)

	require.Error(t, lw.FeatureCheck(license.FeatureMultiregionDeployments, true))
	require.Len(t, lw.logTimes, 1)
	t1 := lw.logTimes[license.FeatureMultiregionDeployments]
	require.NotNil(t, t1)

	// Fire another feature check
	require.Error(t, lw.FeatureCheck(license.FeatureMultiregionDeployments, true))

	t2 := lw.logTimes[license.FeatureMultiregionDeployments]
	require.NotNil(t, t2)

	require.Equal(t, t1, t2)
}

func TestLicenseWatcher_ExpiredLicense(t *testing.T) {
	t.Parallel()

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 1
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanupS1()
	testutil.WaitForLeader(t, s1.RPC)
	state := s1.State()
	lw := s1.EnterpriseState.licenseWatcher

	// Set expiration time
	newLicense := license.NewTestLicense(license.TestGovernancePolicyFlags())
	newLicense.License.ExpirationTime = time.Now().Add(2 * time.Second)
	newLicense.License.TerminationTime = time.Now().Add(2 * time.Second)
	signed, err := newLicense.License.License.SignedString(newLicense.TestPrivateKey)

	require.NoError(t, err)
	newLicense.Signed = signed

	stored := &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1000),
	}
	previousID := previousID(t, lw)
	state.UpsertLicense(1000, stored)
	waitForLicense(t, lw, previousID)

	testutil.WaitForResult(func() (bool, error) {
		if lw.Features() == license.FeatureNone {
			return true, nil
		}
		return false, fmt.Errorf("expected no features on expired license")
	}, func(err error) {
		require.FailNow(t, err.Error())
	})
}

// TestLicenseWatcher_InitLicense ensures the startup license is temporary
func TestLicenseWatcher_InitLicense(t *testing.T) {
	t.Parallel()

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanupS1()
	lw := s1.EnterpriseState.licenseWatcher

	require.True(t, lw.License().Temporary)
}

// TestLicenseWatcher_InitLicense tests that an existing raft license is
// loaded during startup
func TestLicenseWatcher_Init_LoadRaft(t *testing.T) {
	t.Parallel()
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
			preventStart:      true,
		}
	})
	defer cleanupS1()
	state := s1.State()
	lw := s1.EnterpriseState.licenseWatcher

	// Set the tmp license create time in the past
	err := state.TmpLicenseSetBarrier(10, &structs.TmpLicenseBarrier{
		CreateTime: time.Now().Add(-24 * time.Hour).UnixNano()})
	require.NoError(t, err)

	// Set expiration time and re-sign
	existingLicense := license.NewTestLicense(license.TestGovernancePolicyFlags())
	existingLicense.License.CustomerID = "valid-license"
	signed, err := existingLicense.License.License.SignedString(existingLicense.TestPrivateKey)

	require.NoError(t, err)
	existingLicense.Signed = signed

	require.False(t, existingLicense.License.Temporary)

	// Store the valid license in raft
	stored := &structs.StoredLicense{
		Signed:      existingLicense.Signed,
		CreateIndex: uint64(1000),
	}
	require.NoError(t, state.UpsertLicense(1000, stored))

	prev := lw.License().LicenseID
	// start license watcher
	lw.preventStart = false
	lw.start(s1.shutdownCtx)

	waitForLicense(t, lw, prev)
	// Ensure we have the license from raft
	require.False(t, lw.License().Temporary)
	require.Equal(t, "valid-license", lw.License().CustomerID)
}

// TestLicenseWatcher_Init_ExpiredTemp_Shutdown asserts that
// if a cluster is too old for a temporary license, and a valid one
// isn't applied, the agent shuts down
func TestLicenseWatcher_Init_ExpiredTemp_Shutdown(t *testing.T) {
	t.Parallel()
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
			preventStart:      true,
		}
	})
	defer cleanupS1()
	state := s1.State()
	lw := s1.EnterpriseState.licenseWatcher

	executed := make(chan struct{})
	shutdown := func() error { close(executed); return nil }

	lw.shutdownCallback = shutdown
	lw.expiredTmpGrace = 50 * time.Millisecond

	// Set the tmp license create time in the past
	err := state.TmpLicenseSetBarrier(10, &structs.TmpLicenseBarrier{
		CreateTime: time.Now().Add(-24 * time.Hour).UnixNano()})
	require.NoError(t, err)

	// Allow watcher to start
	lw.preventStart = false
	lw.start(s1.shutdownCtx)

	select {
	case <-executed:
		// shutdown properly
	case <-time.After(2 * time.Second):
		require.Fail(t, "timeout waiting for shutdown")
	}
}

// TestLicenseWatcher_Init_ExpiredTemp_ShutdownCancelled asserts that
// if a cluster is too old for a temporary license, and a valid one
// is applied, the agent does not shutdown
func TestLicenseWatcher_Init_ExpiredTemp_Shutdown_Cancelled(t *testing.T) {
	t.Parallel()

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
			preventStart:      true,
		}
	})
	defer cleanupS1()
	state := s1.State()
	lw := s1.EnterpriseState.licenseWatcher

	executed := make(chan struct{})
	shutdown := func() error { close(executed); return nil }

	lw.shutdownCallback = shutdown
	lw.expiredTmpGrace = 2 * time.Second

	// Set the tmp license create time in the past
	err := state.TmpLicenseSetBarrier(10, &structs.TmpLicenseBarrier{
		CreateTime: time.Now().Add(-24 * time.Hour).UnixNano()})
	require.NoError(t, err)

	lw.preventStart = false
	lw.start(s1.shutdownCtx)

	// apply a new license
	newLicense := license.NewTestLicense(license.TestGovernancePolicyFlags())
	newLicense.License.ExpirationTime = time.Now().Add(10 * time.Minute)
	newLicense.License.TerminationTime = time.Now().Add(10 * time.Minute)
	newLicense.License.LicenseID = uuid.Generate()
	newSigned, err := newLicense.License.License.SignedString(newLicense.TestPrivateKey)
	require.NoError(t, err)

	newLicense.Signed = newSigned

	stored := &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1001),
	}
	state.UpsertLicense(1001, stored)

	success := make(chan struct{})
	go func() {
		select {
		case <-lw.monitorTmpExpCtx.Done():
			close(success)
			// properly avoided shutdown
		case <-executed:
			require.Fail(t, "expected shutddown to be prevented")
		}
	}()

	select {
	case <-success:
		// success
	case <-time.After(5 * time.Second):
		require.Fail(t, "timeout waiting for shutdown")
	}
}

// TestTestLicenseWatcher_Init_ExpiredValid_License tests the behavior of loading
// an expired license from raft. go-licensing will reject the update, leaving
// the temporary license in place.
func TestLicenseWatcher_Init_ExpiredValid_License(t *testing.T) {
	t.Parallel()
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
			preventStart:      true,
		}
	})
	defer cleanupS1()
	state := s1.State()
	lw := s1.EnterpriseState.licenseWatcher

	lw.preventStart = false
	lw.start(s1.shutdownCtx)

	// Set expiration time
	existingLicense := license.NewTestLicense(license.TestGovernancePolicyFlags())
	existingLicense.License.ExpirationTime = time.Now().Add(-10 * time.Minute)
	existingLicense.License.TerminationTime = time.Now().Add(-10 * time.Minute)
	signed, err := existingLicense.License.License.SignedString(existingLicense.TestPrivateKey)

	require.NoError(t, err)
	existingLicense.Signed = signed

	stored := &structs.StoredLicense{
		Signed:      existingLicense.Signed,
		CreateIndex: uint64(1000),
	}
	prevID := previousID(t, lw)
	state.UpsertLicense(1000, stored)

	// TODO: (drew) go-licensing support setting expired licenses
	// The existingLicense will not be set since it is expired
	require.Equal(t, lw.License().LicenseID, "temporary-license")

	// Test that we can apply a new valid license

	// Set expiration time
	newLicense := license.NewTestLicense(license.TestGovernancePolicyFlags())
	newLicense.License.ExpirationTime = time.Now().Add(10 * time.Minute)
	newLicense.License.TerminationTime = time.Now().Add(10 * time.Minute)
	newLicense.License.LicenseID = uuid.Generate()
	newSigned, err := newLicense.License.License.SignedString(newLicense.TestPrivateKey)
	require.NoError(t, err)

	newLicense.Signed = newSigned

	stored = &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1001),
	}
	prevID = previousID(t, lw)
	state.UpsertLicense(1001, stored)
	waitForLicense(t, lw, prevID)

	require.Equal(t, newLicense.License.LicenseID, lw.License().LicenseID)
	require.True(t, lw.hasFeature(license.FeatureSentinelPolicies))
}

func TestTempLicenseTooOld(t *testing.T) {
	c1 := time.Now()
	require.False(t, tmpLicenseTooOld(c1))

	// cluster 10 min old
	c2 := time.Now().Add(-10 * time.Minute)
	require.False(t, tmpLicenseTooOld(c2))

	// cluster near expired
	c3 := time.Now().Add(-temporaryLicenseTimeLimit).Add(1 * time.Minute)
	require.False(t, tmpLicenseTooOld(c3))

	// cluster too old
	c4 := time.Now().Add(-24 * time.Hour)
	require.True(t, tmpLicenseTooOld(c4))
}

func TestTempLicense_Cluster_LicenseMeta(t *testing.T) {
	t.Parallel()

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 3
	})
	defer cleanupS1()
	lw1 := s1.EnterpriseState.licenseWatcher

	s1s := s1.State()

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 3
	})
	defer cleanupS2()
	s2s := s2.State()
	lw2 := s2.EnterpriseState.licenseWatcher

	s3, cleanupS3 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 3
	})
	defer cleanupS3()
	s3s := s3.State()
	lw3 := s3.EnterpriseState.licenseWatcher

	// No servers should have tmp metadata before leadership
	// Servers should have features / temporary license while waiting
	s1Meta, err := s1s.TmpLicenseBarrier(nil)
	require.NoError(t, err)
	require.Nil(t, s1Meta)
	require.True(t, lw1.hasFeature(license.FeatureAuditLogging))

	s2Meta, err := s2s.TmpLicenseBarrier(nil)
	require.NoError(t, err)
	require.Nil(t, s2Meta)
	require.True(t, lw2.hasFeature(license.FeatureAuditLogging))

	s3Meta, err := s3s.TmpLicenseBarrier(nil)
	require.NoError(t, err)
	require.Nil(t, s3Meta)
	require.True(t, lw3.hasFeature(license.FeatureAuditLogging))

	// Join servers and establish leadership
	TestJoin(t, s1, s2, s3)
	testutil.WaitForLeader(t, s1.RPC)

	var t1, t2, t3 int64
	testutil.WaitForResult(func() (bool, error) {
		meta, err := s1.State().TmpLicenseBarrier(nil)
		require.NoError(t, err)

		if meta == nil {
			return false, fmt.Errorf("expected tmp license barrier")
		}
		t1 = meta.CreateTime
		return true, nil
	}, func(err error) {
		require.FailNow(t, err.Error())
	})

	testutil.WaitForResult(func() (bool, error) {
		meta, err := s2.State().TmpLicenseBarrier(nil)
		require.NoError(t, err)

		if meta == nil {
			return false, fmt.Errorf("expected tmp license barrier")
		}
		t2 = meta.CreateTime
		return true, nil
	}, func(err error) {
		require.FailNow(t, err.Error())
	})

	testutil.WaitForResult(func() (bool, error) {
		meta, err := s3.State().TmpLicenseBarrier(nil)
		require.NoError(t, err)

		if meta == nil {
			return false, fmt.Errorf("expected tmp license barrier")
		}
		t3 = meta.CreateTime
		return true, nil
	}, func(err error) {
		require.FailNow(t, err.Error())
	})

	require.Equal(t, t1, t2)
	require.Equal(t, t2, t3)
}

// TestLicenseWatcher_StateRestore ensures that an already running
// license watcher will be be notified and properly update if a server restores
// it's state from a snapshot
func TestLicenseWatcher_StateRestore(t *testing.T) {
	t.Parallel()
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
			preventStart:      true,
		}
	})
	defer cleanupS1()

	// s2 will be used to store license and create a snapshot for s1 to restore from
	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
			preventStart:      true,
		}
	})
	defer cleanupS2()

	s1State := s1.State()
	s2State := s2.State()

	// executed and shutdown are used to track if the license watcher
	// intends to shutdown a server
	executed := make(chan struct{})
	shutdown := func() error { close(executed); return nil }

	lw := s1.EnterpriseState.licenseWatcher
	lw.shutdownCallback = shutdown
	lw.expiredTmpGrace = 1 * time.Second

	// Set the tmp license create time in the past
	err := s1State.TmpLicenseSetBarrier(10, &structs.TmpLicenseBarrier{
		CreateTime: time.Now().Add(-24 * time.Hour).UnixNano()})
	require.NoError(t, err)

	err = s2State.TmpLicenseSetBarrier(10, &structs.TmpLicenseBarrier{
		CreateTime: time.Now().Add(-24 * time.Hour).UnixNano()})
	require.NoError(t, err)

	// Start the license watcher
	lw.preventStart = false
	lw.start(s1.shutdownCtx)

	// Store a valid license on s2
	newLicense := licenseFile("new-license", time.Now(), time.Now().Add(1*time.Hour))
	stored := &structs.StoredLicense{
		Signed:      newLicense,
		CreateIndex: uint64(1000),
	}
	s2State.UpsertLicense(1001, stored)
	require.NoError(t, err)

	// Create a new snapshot from s2
	snap, err := s2.fsm.Snapshot()
	require.NoError(t, err)

	// Persist the snapshot
	buf := bytes.NewBuffer(nil)
	sink := &MockSink{buf, false}
	require.NoError(t, snap.Persist(sink))

	// Ensure the current license is still the temporary one
	require.Equal(t, lw.License().LicenseID, "temporary-license")

	// Restore the snapshot into s1 while the license watcher is running
	err = s1.fsm.Restore(sink)
	require.NoError(t, err)

	waitForLicense(t, lw, "temporary-license")

	require.Equal(t, lw.License().LicenseID, "new-license")

	select {
	case <-executed:
		require.Fail(t, "license watcher should not have closed")
	default:
		// Pass
	}
}

// TestLicenseWatcher_start checks that the expected license is used when the
// license watcher first starts.
func TestLicenseWatcher_start(t *testing.T) {
	t.Parallel()
	cases := []struct {
		desc              string
		envLicense        string
		fileLicense       string
		raftLicense       string
		expectedLicenseID string
	}{
		{
			desc:              "temporary license - newly initialized cluster",
			expectedLicenseID: "temporary-license",
		},
		{
			desc:              "file license loaded from file",
			fileLicense:       licenseFile("file-id", time.Now(), time.Now().Add(1*time.Hour)),
			expectedLicenseID: "file-id",
		},
		{
			desc:              "file license loaded from env",
			envLicense:        licenseFile("env-id", time.Now(), time.Now().Add(1*time.Hour)),
			expectedLicenseID: "env-id",
		},
		{
			desc:              "env and file chooses env",
			fileLicense:       licenseFile("file-id", time.Now(), time.Now().Add(1*time.Hour)),
			envLicense:        licenseFile("env-id", time.Now(), time.Now().Add(1*time.Hour)),
			expectedLicenseID: "env-id",
		},
		{
			desc:              "raft license with newer issue date is used over file license with older issue date",
			fileLicense:       licenseFile("file-id", time.Now().Add(-24*time.Hour), time.Now().Add(1*time.Hour)),
			raftLicense:       licenseFile("raft-id", time.Now(), time.Now().Add(2*time.Hour)),
			expectedLicenseID: "raft-id",
		},
		{
			desc:              "raft license with older issue date is ignored over file license with newer issue date",
			fileLicense:       licenseFile("file-id", time.Now(), time.Now().Add(2*time.Hour)),
			raftLicense:       licenseFile("raft-id", time.Now().Add(-24*time.Hour), time.Now().Add(1*time.Hour)),
			expectedLicenseID: "file-id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			var tempfilePath string
			if tc.fileLicense != "" {
				f, err := ioutil.TempFile("", "licensewatcher")
				require.NoError(t, err)
				_, err = io.WriteString(f, tc.fileLicense)
				require.NoError(t, err)
				require.NoError(t, f.Close())

				defer os.Remove(f.Name())
				tempfilePath = f.Name()
			}

			server, cleanup := TestServer(t, func(c *Config) {
				c.LicenseEnv = tc.envLicense
				c.LicensePath = tempfilePath
				c.LicenseConfig = &LicenseConfig{
					AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
				}
			})
			defer cleanup()

			if tc.raftLicense != "" {
				state := server.State()
				require.NotNil(t, state)
				require.NoError(t, state.UpsertLicense(1000, &structs.StoredLicense{Signed: tc.raftLicense}))
			}

			require.Eventually(t, func() bool {
				license := server.EnterpriseState.licenseWatcher.License()
				return tc.expectedLicenseID == license.LicenseID
			}, time.Second, 10*time.Millisecond, fmt.Sprintf("Expected license ID to equal %s", tc.expectedLicenseID))

			if tc.raftLicense != "" {
				timeout := time.After(500 * time.Millisecond)
			OUT:
				for {
					select {
					case <-timeout:
						break OUT
					case <-time.After(100 * time.Millisecond):
						license := server.EnterpriseState.licenseWatcher.License()
						require.Equal(t, tc.expectedLicenseID, license.LicenseID)
					}
				}
			}

		})
	}
}

// TestLicenseWatcher_Reload_EmptyConfig asserts that reloading the license
// watcher with an empty config no-ops
func TestLicenseWatcher_Reload_EmptyConfig(t *testing.T) {
	t.Parallel()

	server, cleanup := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanup()

	initLicense := server.EnterpriseState.License()
	require.NotNil(t, initLicense)
	require.True(t, initLicense.Temporary)

	reloadCfg := &LicenseConfig{}

	require.NoError(t, server.EnterpriseState.licenseWatcher.Reload(reloadCfg))

	lic := server.EnterpriseState.License()
	require.NotNil(t, lic)
	require.True(t, lic.Temporary)

	// Ensure the license did not change
	require.Equal(t, initLicense, lic)
}

// TestLicenseWatcher_Reload_FileNewer ensures that when reloading a newer file
// license is used
func TestLicenseWatcher_Reload_FileNewer(t *testing.T) {
	t.Parallel()

	server, cleanup := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanup()

	initLicense := server.EnterpriseState.License()
	require.NotNil(t, initLicense)
	require.True(t, initLicense.Temporary)

	file := licenseFile("reload-id", time.Now(), time.Now().Add(1*time.Hour))
	f, err := ioutil.TempFile("", "licensewatcher")
	require.NoError(t, err)
	_, err = io.WriteString(f, file)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	defer os.Remove(f.Name())

	reloadCfg := &LicenseConfig{
		LicensePath: f.Name(),
	}

	require.NoError(t, server.EnterpriseState.licenseWatcher.Reload(reloadCfg))

	require.Eventually(t, func() bool {
		license := server.EnterpriseState.licenseWatcher.License()
		return "reload-id" == license.LicenseID
	}, time.Second, 10*time.Millisecond, fmt.Sprintf("Expected license ID to equal %s", "reload-id"))
}

// TestLicenseWatcher_Reload_RaftNewer ensures that reloading the license
// watcher with an older file license does not replace the newer one in raft.
func TestLicenseWatcher_Reload_RaftNewer(t *testing.T) {
	t.Parallel()

	server, cleanup := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanup()

	initLicense := server.EnterpriseState.License()
	require.NotNil(t, initLicense)
	require.True(t, initLicense.Temporary)

	raftLicense := licenseFile("raft-id", time.Now(), time.Now().Add(1*time.Hour))
	envLicense := licenseFile("reload-id", time.Now().Add(-2*time.Hour), time.Now().Add(1*time.Hour))

	state := server.State()
	require.NotNil(t, state)
	require.NoError(t, state.UpsertLicense(1000, &structs.StoredLicense{Signed: raftLicense}))

	require.Never(t, func() bool {
		license := server.EnterpriseState.licenseWatcher.License()
		return "reload-id" == license.LicenseID
	}, time.Second, 10*time.Millisecond, fmt.Sprintf("Expected license ID to not equal %s", "reload-id"))

	reloadCfg := &LicenseConfig{
		LicenseEnvBytes: envLicense,
	}

	// Reload should error
	require.Error(t, server.EnterpriseState.licenseWatcher.Reload(reloadCfg))

	license := server.EnterpriseState.licenseWatcher.License()
	require.Equal(t, "raft-id", license.LicenseID)
}

// TestLicenseWatcher_Reload_RaftSetForcibly ensures that reloading the license
// watcher with a newer file license does not replace the older one in raft if
// it was forcibly set.
func TestLicenseWatcher_Reload_RaftSetForcibly(t *testing.T) {
	t.Parallel()

	server, cleanup := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanup()

	initLicense := server.EnterpriseState.License()
	require.NotNil(t, initLicense)
	require.True(t, initLicense.Temporary)

	raftLicense := licenseFile("raft-id", time.Now().Add(-2*time.Hour), time.Now().Add(1*time.Hour))
	envLicense := licenseFile("reload-id", time.Now(), time.Now().Add(1*time.Hour))

	state := server.State()
	require.NotNil(t, state)
	require.NoError(t, state.UpsertLicense(1000, &structs.StoredLicense{Signed: raftLicense, Force: true}))

	require.Never(t, func() bool {
		license := server.EnterpriseState.licenseWatcher.License()
		return "reload-id" == license.LicenseID
	}, time.Second, 10*time.Millisecond, fmt.Sprintf("Expected license ID to not equal %s", "reload-id"))

	reloadCfg := &LicenseConfig{
		LicenseEnvBytes: envLicense,
	}

	// Reload should error
	require.Error(t, server.EnterpriseState.licenseWatcher.Reload(reloadCfg))

	license := server.EnterpriseState.licenseWatcher.License()
	require.Equal(t, "raft-id", license.LicenseID)
}

// TestLicenseWatcher_ExpiredLicense_SetOlderValidLicense ensures that a server
// with an expired license can set an older, valid license
func TestLicenseWatcher_ExpiredLicense_SetOlderValidLicense(t *testing.T) {
	t.Parallel()

	envLicense := licenseFile("env-id", time.Now().Add(-1*time.Hour), time.Now().Add(1*time.Second))
	server, cleanup := TestServer(t, func(c *Config) {
		c.LicenseEnv = envLicense
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanup()

	lic := server.EnterpriseState.License()
	require.Equal(t, "env-id", lic.LicenseID)

	// Ensure the license is expired and no features available
	require.Eventually(t, func() bool {
		return server.EnterpriseState.Features() == uint64(0)
	}, 2*time.Second, 100*time.Millisecond, fmt.Sprintf("Expected license to expire"))

	oldButValid := licenseFile("old-id", time.Now().Add(-1*24*365*time.Hour), time.Now().Add(5*time.Hour))

	require.NoError(t, server.EnterpriseState.SetLicense(oldButValid, false))

	require.Equal(t, "old-id", server.EnterpriseState.License().LicenseID)
}

// TestLicenseWatcher_SetLicense_Propagation ensures that a license and its
// force field are properly propagated
func TestLicenseWatcher_SetLicense_Propagation(t *testing.T) {
	t.Parallel()

	fileLic := licenseFile("start-id", time.Now(), time.Now().Add(1*time.Hour))
	forceLic := licenseFile("force-id", time.Now().Add(-1*time.Minute), time.Now().Add(1*time.Hour))

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.LicenseEnv = fileLic
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanupS1()

	// s2 will be used to store license and create a snapshot for s1 to restore from
	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.LicenseEnv = fileLic
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})
	defer cleanupS2()

	TestJoin(t, s1, s2)
	testutil.WaitForLeader(t, s1.RPC)
	testutil.WaitForLeader(t, s2.RPC)

	require.Equal(t, "start-id", s1.EnterpriseState.License().LicenseID)
	require.Equal(t, "start-id", s2.EnterpriseState.License().LicenseID)

	if s1.IsLeader() {
		require.NoError(t, s1.EnterpriseState.SetLicense(forceLic, true))
	} else if s2.IsLeader() {
		require.NoError(t, s2.EnterpriseState.SetLicense(forceLic, true))
	} else {
		require.FailNow(t, "expected there to be a leader")
	}

	testutil.WaitForResult(func() (bool, error) {
		out1, err := s1.State().License(nil)
		require.NoError(t, err)

		if forceLic != out1.Signed {
			return false, fmt.Errorf("expected s1 license to update want %v got %v", forceLic, out1.Signed)
		}
		require.True(t, out1.Force)

		out2, err := s2.State().License(nil)
		require.NoError(t, err)

		if forceLic != out2.Signed {
			return false, fmt.Errorf("expected s2 license to update, want %v got %v", forceLic, out2.Signed)
		}
		require.True(t, out2.Force)

		return true, nil
	}, func(err error) {
		require.Fail(t, err.Error())
	})

}

func licenseFile(id string, issue, exp time.Time) string {
	l := &licensing.License{
		LicenseID:       id,
		CustomerID:      "test customer id",
		InstallationID:  "*",
		Product:         "nomad",
		IssueTime:       issue,
		StartTime:       issue,
		ExpirationTime:  exp,
		TerminationTime: exp,
		Flags:           map[string]interface{}{},
	}
	signed, _ := l.SignedString(nomadLicense.TestPrivateKey)
	return signed

}
