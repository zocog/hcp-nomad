// +build ent

package agent

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/nomad/audit"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/stretchr/testify/require"
)

func TestAuditHTTPHandler(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		path         string
		expectedCode int
		failAudit    bool
		body         string
	}{
		{
			desc:         "wrapped endpoint success",
			path:         "/v1/agent/health",
			expectedCode: 200,
		},
		{
			desc:         "audit endpoint failure",
			path:         "/v1/agent/health",
			expectedCode: 500,
			failAudit:    true,
			body:         "event not written to enough sinks",
		},
	}

	s := makeHTTPServer(t, nil)
	defer s.Shutdown()

	tmpDir, err := ioutil.TempDir("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	auditor, err := audit.NewAuditor(&audit.Config{
		Enabled: true,
		Sinks: []audit.SinkConfig{
			{
				Name:              "file",
				Type:              audit.FileSink,
				Format:            audit.JSONFmt,
				DeliveryGuarantee: audit.Enforced,
				Path:              tmpDir,
				FileName:          "audit.log",
			},
		},
	})
	require.NoError(t, err)
	s.eventer = auditor

	handler := func(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
		return &structs.Job{Name: "foo"}, nil
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			if tc.failAudit {
				path := filepath.Join(tmpDir, "audit.log")
				_, err := os.Create(path)
				require.NoError(t, err)

				err = os.Chmod(path, 000)
				require.NoError(t, err)
			}
			resp := httptest.NewRecorder()
			req, err := http.NewRequest("GET", tc.path, nil)
			require.NoError(t, err)

			s.Server.wrap(handler)(resp, req)
			require.Equal(t, tc.expectedCode, resp.Code)
			require.Equal(t, tc.body, resp.Body.String())
		})
	}
}
