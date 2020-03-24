// +build ent

package agent

import (
	"errors"
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

func TestAuditWrapHTTPHandler(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		path         string
		handler      func(resp http.ResponseWriter, req *http.Request) (interface{}, error)
		handlerErr   string
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
			desc:         "failure auditing request",
			path:         "/v1/agent/health",
			expectedCode: 500,
			failAudit:    true,
			body:         "event not written to enough sinks",
		},
		{
			desc:         "handler returns error",
			path:         "/v1/agent/health",
			handlerErr:   "error",
			expectedCode: 500,
			failAudit:    false,
			body:         "error",
		},
	}

	s := makeHTTPServer(t, nil)
	defer s.Shutdown()

	parentTestName := t.Name()
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {

			handler := func(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
				if tc.handlerErr != "" {
					return nil, errors.New(tc.handlerErr)
				}
				return &structs.Job{Name: "foo"}, nil
			}

			tmpDir, err := ioutil.TempDir("", parentTestName+"-"+tc.desc)
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
			// Set the server eventer
			s.auditor = auditor

			// If we want to intentionally fail, make it so the auditor
			// cannot write to the tmp file.
			if tc.failAudit {
				// Create the audit log first, since the file sink
				// creates on first write
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

			if tc.body != "" {
				require.Equal(t, tc.body, resp.Body.String())
			}
		})
	}
}

func TestAuditNonJSONHandler(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		path         string
		handlerErr   string
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
			desc:         "failure auditing request",
			path:         "/v1/agent/health",
			expectedCode: 500,
			failAudit:    true,
			body:         "event not written to enough sinks",
		},
		{
			desc:         "handler returns error",
			path:         "/v1/agent/health",
			expectedCode: 500,
			failAudit:    false,
			handlerErr:   "error",
			body:         "error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			s := makeHTTPServer(t, nil)
			defer s.Shutdown()

			parentTestName := t.Name()

			handler := func(resp http.ResponseWriter, req *http.Request) ([]byte, error) {
				if tc.handlerErr != "" {
					return nil, errors.New(tc.handlerErr)
				}
				return []byte("response"), nil
			}

			tmpDir, err := ioutil.TempDir("", parentTestName+"-"+tc.desc)
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
			// Set the server eventer
			s.auditor = auditor

			// If we want to intentionally fail, make it so the auditor
			// cannot write to the tmp file.
			if tc.failAudit {
				// Create the audit log first, since the file sink
				// creates on first write
				path := filepath.Join(tmpDir, "audit.log")
				_, err := os.Create(path)
				require.NoError(t, err)

				err = os.Chmod(path, 000)
				require.NoError(t, err)
			}

			resp := httptest.NewRecorder()
			req, err := http.NewRequest("GET", tc.path, nil)
			require.NoError(t, err)

			s.Server.wrapNonJSON(handler)(resp, req)
			require.Equal(t, tc.expectedCode, resp.Code)

			if tc.body != "" {
				require.Equal(t, tc.body, resp.Body.String())
			}
		})
	}
}
