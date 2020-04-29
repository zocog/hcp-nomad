// +build ent

package agent

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/stretchr/testify/require"
)

func TestOperator_GetLicense(t *testing.T) {
	t.Parallel()

	httpTest(t, nil, func(s *TestAgent) {
		l := mock.StoredLicense()
		state := s.server.State()

		require.NoError(t, state.UpsertLicense(999, l))

		body := bytes.NewBuffer(nil)
		req, err := http.NewRequest("GET", "/v1/operator/license", body)
		require.NoError(t, err)

		resp := httptest.NewRecorder()
		lic, err := s.Server.OperatorLicenseRequest(resp, req)
		require.NoError(t, err)
		require.Equal(t, resp.Code, 200)

		out, ok := lic.(structs.LicenseGetResponse)
		require.True(t, ok)
		require.Equal(t, out.License.Signed, l.Signed)
	})
}

func TestOperator_PutLicense(t *testing.T) {
	t.Parallel()

	// Test Invalid key
	httpTest(t, nil, func(s *TestAgent) {
		l := mock.StoredLicense()
		state := s.server.State()

		require.NoError(t, state.UpsertLicense(999, l))

		body := bytes.NewBuffer([]byte("YIUASDIasdfj1238AYIadsan="))
		req, err := http.NewRequest("PUT", "/v1/operator/license", body)
		require.NoError(t, err)

		resp := httptest.NewRecorder()
		lic, err := s.Server.OperatorLicenseRequest(resp, req)
		require.Error(t, err)
		require.Nil(t, lic)
	})

	// Test Valid Key
	httpTest(t, nil, func(s *TestAgent) {
		tl := nomadLicense.NewTestLicense()

		l := mock.StoredLicense()
		l.Signed = tl.Signed

		body := bytes.NewBuffer([]byte(l.Signed))
		req, err := http.NewRequest("PUT", "/v1/operator/license", body)
		require.NoError(t, err)

		resp := httptest.NewRecorder()
		lic, err := s.Server.OperatorLicenseRequest(resp, req)
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
		lic, err := s.Server.OperatorLicenseRequest(resp, req)
		require.Error(t, err)
		require.Nil(t, lic)

		codedErr, ok := err.(HTTPCodedError)
		require.True(t, ok)
		require.Equal(t, codedErr.Code(), 404)
	})
}
