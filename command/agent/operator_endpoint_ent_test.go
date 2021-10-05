//go:build ent
// +build ent

package agent

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/nomad"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/require"
)

func TestOperator_GetLicense(t *testing.T) {
	t.Parallel()

	cb := func(c *Config) {
		c.NomadConfig.BootstrapExpect = 1
		c.NomadConfig.LicenseConfig = &nomad.LicenseConfig{
			AdditionalPubKeys: []string{
				base64.StdEncoding.EncodeToString(license.TestPublicKey)},
		}

	}
	start := time.Now()

	httpTest(t, cb, func(s *TestAgent) {

		testutil.WaitForLeader(t, s.RPC)

		body := bytes.NewBuffer(nil)
		req, err := http.NewRequest("GET", "/v1/operator/license", body)
		require.NoError(t, err)

		resp := httptest.NewRecorder()
		lic, err := s.Server.LicenseRequest(resp, req)
		require.NoError(t, err)
		require.Equal(t, resp.Code, 200)

		out, ok := lic.(api.LicenseReply)
		require.True(t, ok)
		require.True(t, out.License.ExpirationTime.After(start.Add(1*time.Hour)))
		require.Equal(t, "governance-policy", out.License.Modules[0])
		require.Equal(t, "multicluster-and-efficiency", out.License.Modules[1])
	})
}

func TestOperator_License_UnknownVerb(t *testing.T) {
	t.Parallel()

	httpTest(t, nil, func(s *TestAgent) {
		body := bytes.NewBuffer(nil)
		req, err := http.NewRequest("POST", "/v1/operator/license", body)
		require.NoError(t, err)

		resp := httptest.NewRecorder()
		lic, err := s.Server.LicenseRequest(resp, req)
		require.Error(t, err)
		require.Nil(t, lic)

		codedErr, ok := err.(HTTPCodedError)
		require.True(t, ok)
		require.Equal(t, codedErr.Code(), 405)
	})
}
