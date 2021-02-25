// +build ent

package nomad

import (
	"encoding/base64"
	"fmt"
	"testing"

	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/nomad/testutil"
)

func licenseCallback(cfg *Config) {
	cfg.LicenseConfig = &LicenseConfig{
		AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
	}
}

func licensedServer(t *testing.T, signedLicense string) (*Server, func()) {
	s, cleanup := TestServer(t, func(c *Config) {
		c.NumSchedulers = 0 // Prevent automatic dequeue
		licenseCallback(c)
	})
	testutil.WaitForLeader(t, s.RPC)
	err := s.EnterpriseState.SetLicense(signedLicense, false)
	require.NoError(t, err)
	testutil.WaitForResult(func() (bool, error) {
		out := s.EnterpriseState.licenseWatcher.License()
		require.NoError(t, err)
		if _, ok := out.License.Flags["temporary"]; !ok {
			return true, nil
		}
		return false, fmt.Errorf("license still temporary")
	}, func(err error) {
		require.Failf(t, "failed to find updated license", err.Error())
	})
	return s, cleanup
}
