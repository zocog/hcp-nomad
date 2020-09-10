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
	s, cleanup := TestServer(t, licenseCallback)
	testutil.WaitForLeader(t, s.RPC)
	_, err := s.EnterpriseState.licenseWatcher.SetLicense(signedLicense)
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
