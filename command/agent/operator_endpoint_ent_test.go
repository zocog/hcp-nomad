// +build ent

package agent

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/go-memdb"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/nomad"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/require"
)

func TestOperator_GetLicense(t *testing.T) {
	t.Parallel()

	cb := func(c *Config) {
		c.NomadConfig.BootstrapExpect = 1
		c.NomadConfig.LicenseConfig = &nomad.LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(license.TestPublicKey)},
		}

	}
	httpTest(t, cb, func(s *TestAgent) {
		stored, l := mock.StoredLicense()
		state := s.server.State()

		testutil.WaitForLeader(t, s.RPC)

		require.NoError(t, state.UpsertLicense(999, stored))

		testutil.WaitForResult(func() (bool, error) {
			watchSet := memdb.NewWatchSet()
			stored, err := state.License(watchSet)
			return stored != nil, err
		}, func(err error) {
			require.Fail(t, "update should be received from raft", err)
		})

		var out api.LicenseReply
		var ok bool
		testutil.WaitForResult(func() (bool, error) {
			body := bytes.NewBuffer(nil)
			req, err := http.NewRequest("GET", "/v1/operator/license", body)
			require.NoError(t, err)

			resp := httptest.NewRecorder()
			lic, err := s.Server.LicenseRequest(resp, req)
			require.NoError(t, err)
			require.Equal(t, resp.Code, 200)

			out, ok = lic.(api.LicenseReply)
			require.True(t, ok)

			if !out.License.ExpirationTime.Equal(l.ExpirationTime) {
				return false, fmt.Errorf("expected updated license")
			}
			return true, nil
		}, func(err error) {
			require.Fail(t, "failed to receive new license", err)
		})

		require.Equal(t, out.License.CustomerID, l.CustomerID)
		require.Equal(t, out.License.InstallationID, l.InstallationID)
		require.Equal(t, out.License.Product, l.Product)
		require.True(t, out.License.ExpirationTime.Equal(l.ExpirationTime))
		require.True(t, out.License.StartTime.Equal(l.StartTime))
		require.True(t, out.License.TerminationTime.Equal(l.TerminationTime))
		require.True(t, out.License.IssueTime.Equal(l.IssueTime))
	})
}

func TestOperator_PutLicense(t *testing.T) {
	t.Parallel()

	// Test Invalid key
	httpTest(t, nil, func(s *TestAgent) {
		stored, _ := mock.StoredLicense()
		state := s.server.State()

		require.NoError(t, state.UpsertLicense(999, stored))

		body := bytes.NewBuffer([]byte("YIUASDIasdfj1238AYIadsan="))
		req, err := http.NewRequest("PUT", "/v1/operator/license", body)
		require.NoError(t, err)

		resp := httptest.NewRecorder()
		lic, err := s.Server.LicenseRequest(resp, req)
		require.Error(t, err)
		require.Nil(t, lic)
	})

	// Test Valid Key
	httpTest(t, nil, func(s *TestAgent) {
		stored, _ := mock.StoredLicense()

		body := bytes.NewBuffer([]byte(stored.Signed))
		req, err := http.NewRequest("PUT", "/v1/operator/license", body)
		require.NoError(t, err)

		resp := httptest.NewRecorder()
		lic, err := s.Server.LicenseRequest(resp, req)
		require.Error(t, err)
		require.Nil(t, lic)
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
