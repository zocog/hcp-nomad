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
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/nomad"
	"github.com/hashicorp/nomad/testutil"
	"github.com/shoenig/test/must"
)

func TestOperator_GetLicense(t *testing.T) {
	ci.Parallel(t)

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
		must.NoError(t, err)

		resp := httptest.NewRecorder()
		lic, err := s.Server.LicenseRequest(resp, req)
		must.NoError(t, err)
		must.Eq(t, resp.Code, 200)

		out, ok := lic.(api.LicenseReply)
		must.True(t, ok)
		must.True(t, out.License.ExpirationTime.After(start.Add(1*time.Hour)))
		must.Eq(t, "governance-policy", out.License.Modules[0])
		must.Eq(t, "multicluster-and-efficiency", out.License.Modules[1])
	})
}

func TestOperator_License_UnknownVerb(t *testing.T) {
	ci.Parallel(t)

	httpTest(t, nil, func(s *TestAgent) {
		body := bytes.NewBuffer(nil)
		req, err := http.NewRequest("POST", "/v1/operator/license", body)
		must.NoError(t, err)

		resp := httptest.NewRecorder()
		lic, err := s.Server.LicenseRequest(resp, req)
		must.Error(t, err)
		must.Nil(t, lic)

		codedErr, ok := err.(HTTPCodedError)
		must.True(t, ok)
		must.Eq(t, codedErr.Code(), 405)
	})
}
