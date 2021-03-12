// +build ent

package nomad

import (
	"encoding/base64"
	"testing"
	"time"

	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/require"
)

func TestSyncLeaderLicense_NewFile(t *testing.T) {
	t.Parallel()

	raftLicense := licenseFile("raft-id", time.Now().Add(-100*time.Hour), time.Now().Add(1*time.Hour))
	fileLicense := licenseFile("license-id", time.Now(), time.Now().Add(1*time.Hour))

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseEnv = fileLicense
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
		c.BootstrapExpect = 2
	})
	defer cleanupS1()

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.LicenseEnv = fileLicense
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
		c.BootstrapExpect = 2
	})
	defer cleanupS2()

	// Set s1 s2 raft license
	require.NoError(t, s1.State().UpsertLicense(100, &structs.StoredLicense{Signed: raftLicense}))
	require.NoError(t, s2.State().UpsertLicense(100, &structs.StoredLicense{Signed: raftLicense}))

	TestJoin(t, s1, s2)
	testutil.WaitForLeader(t, s1.RPC)
	testutil.WaitForLeader(t, s2.RPC)

	out, err := s1.State().License(nil)
	require.NoError(t, err)
	require.Equal(t, fileLicense, out.Signed)

	out, err = s2.State().License(nil)
	require.NoError(t, err)
	require.Equal(t, fileLicense, out.Signed)
}

// TestSyncLeaderLicense_RaftForciblySet ensures that the license in raft is
// not overwritten during syncLeaderLicense if the raft license was forcibly
// set
func TestSyncLeaderLicense_RaftForciblySet(t *testing.T) {
	t.Parallel()

	raftLicense := licenseFile("raft-id", time.Now().Add(-100*time.Hour), time.Now().Add(1*time.Hour))
	fileLicense := licenseFile("license-id", time.Now(), time.Now().Add(1*time.Hour))

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseEnv = fileLicense
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
		c.BootstrapExpect = 2
	})
	defer cleanupS1()

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.LicenseEnv = fileLicense
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
		c.BootstrapExpect = 2
	})
	defer cleanupS2()

	// Set s1 s2 raft license
	stored := &structs.StoredLicense{Signed: raftLicense, Force: true}
	require.NoError(t, s1.State().UpsertLicense(100, stored))
	require.NoError(t, s2.State().UpsertLicense(100, stored))

	TestJoin(t, s1, s2)
	testutil.WaitForLeader(t, s1.RPC)
	testutil.WaitForLeader(t, s2.RPC)

	out, err := s1.State().License(nil)
	require.NoError(t, err)
	require.Equal(t, raftLicense, out.Signed)

	out, err = s2.State().License(nil)
	require.NoError(t, err)
	require.Equal(t, raftLicense, out.Signed)

	s1Lic := s1.EnterpriseState.License()
	require.NotNil(t, s1Lic)
	require.Equal(t, "raft-id", s1Lic.LicenseID)

	s2Lic := s2.EnterpriseState.License()
	require.NotNil(t, s2Lic)
	require.Equal(t, "raft-id", s2Lic.LicenseID)
}

// TestSyncLeaderLicense_RaftForciblySet ensures that the license in raft is
// not overwritten during syncLeaderLicense if the raft license was forcibly
// set
func TestSyncLeaderLicense_EventualConsistency(t *testing.T) {
	t.Parallel()

	initTime := time.Now().Add(-100 * time.Hour)
	expTime := time.Now().Add(24 * 365 * time.Hour)
	s1Lic := licenseFile("s1-license", initTime, expTime)
	s2Lic := licenseFile("s2-license", initTime, expTime)

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseEnv = s1Lic
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
		c.BootstrapExpect = 2
	})
	defer cleanupS1()

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.LicenseEnv = s2Lic
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
		c.BootstrapExpect = 2
	})
	defer cleanupS2()

	TestJoin(t, s1, s2)
	testutil.WaitForLeader(t, s1.RPC)
	testutil.WaitForLeader(t, s2.RPC)

	out, err := s1.State().License(nil)
	require.NoError(t, err)
	require.NotNil(t, out)

	out2, err := s2.State().License(nil)
	require.NoError(t, err)
	require.NotNil(t, out)

	require.Equal(t, out, out2)
}
