// +build ent

package agent

import (
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/nomad/audit"
	"github.com/hashicorp/nomad/command/agent/event"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/stretchr/testify/require"
)

var _ http.Hijacker = &auditResponseWriter{}

func TestAuditWrapHTTPHandler(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		path         string
		handler      func(resp http.ResponseWriter, req *http.Request) (interface{}, error)
		handlerErr   string
		expectedCode int
		auditErr     error
		body         string
	}{
		{
			desc:         "wrapped endpoint success",
			path:         "/v1/metrics",
			expectedCode: 200,
		},
		{
			desc:         "failure auditing request",
			path:         "/v1/metrics",
			expectedCode: 500,
			auditErr:     errors.New("event not written to enough sinks"),
			body:         "event not written to enough sinks",
		},
		{
			desc:         "handler returns error",
			path:         "/v1/metrics",
			handlerErr:   "error",
			expectedCode: 500,
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
				Logger:  testlog.HCLogger(t),
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
				FeatureChecker: &s.server.EnterpriseState,
			})
			require.NoError(t, err)

			// Set the server auditor
			s.auditor = &testAuditor{auditor: auditor, auditErr: tc.auditErr}

			resp := httptest.NewRecorder()
			req, err := http.NewRequest("GET", tc.path, nil)
			require.NoError(t, err)

			s.Server.wrap(handler)(resp, req)
			require.Equal(t, tc.expectedCode, resp.Code)

			if tc.body != "" {
				require.Equal(t, tc.body, resp.Body.String())
			}

			if tc.auditErr == nil {
				// Read from audit log
				dat, err := ioutil.ReadFile(filepath.Join(tmpDir, "audit.log"))
				require.NoError(t, err)
				require.NotEmpty(t, dat)
			}
		})
	}
}

func TestEventFromReq(t *testing.T) {
	s := makeHTTPServer(t, nil)
	defer s.Shutdown()

	req, err := http.NewRequest("GET", "/v1/metrics", nil)
	require.NoError(t, err)

	// Add to request context
	ctx := req.Context()
	reqID := uuid.Generate()
	ctx = context.WithValue(ctx, ContextKeyReqID, reqID)
	req = req.WithContext(ctx)

	auth := &audit.Auth{
		AccessorID: uuid.Generate(),
		Name:       "token name",
		Global:     false,
		CreateTime: time.Now(),
	}

	srv := s.Server
	event := srv.eventFromReq(ctx, req, auth)

	require.Equal(t, srv.agent.GetConfig().AdvertiseAddrs.HTTP, event.Request.NodeMeta["ip"])
	require.Equal(t, audit.OperationReceived, event.Stage)
	require.Equal(t, reqID, event.Request.ID)
	require.Equal(t, auth, event.Auth)
}

func TestAuditNonJSONHandler(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		path         string
		handlerErr   string
		expectedCode int
		auditErr     error
		body         string
	}{
		{
			desc:         "wrapped endpoint success",
			path:         "/v1/metrics",
			expectedCode: 200,
		},
		{
			desc:         "failure auditing request",
			path:         "/v1/metrics",
			expectedCode: 500,
			auditErr:     errors.New("event not written to enough sinks"),
			body:         "event not written to enough sinks",
		},
		{
			desc:         "handler returns error",
			path:         "/v1/metrics",
			expectedCode: 500,
			handlerErr:   "error",
			body:         "error",
		},
	}

	parentTestName := t.Name()
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			s := makeHTTPServer(t, nil)
			defer s.Shutdown()

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
				Logger:  testlog.HCLogger(t),
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
				FeatureChecker: &s.server.EnterpriseState,
			})
			require.NoError(t, err)

			// Set the server auditor
			s.auditor = &testAuditor{auditor: auditor, auditErr: tc.auditErr}

			resp := httptest.NewRecorder()
			req, err := http.NewRequest("GET", tc.path, nil)
			require.NoError(t, err)

			s.Server.wrapNonJSON(handler)(resp, req)
			require.Equal(t, tc.expectedCode, resp.Code)

			if tc.body != "" {
				require.Equal(t, tc.body, resp.Body.String())
			}

			if tc.auditErr == nil {
				// Read from audit log
				dat, err := ioutil.ReadFile(filepath.Join(tmpDir, "audit.log"))
				require.NoError(t, err)
				require.NotEmpty(t, dat)
			}
		})
	}
}

type testAuditor struct {
	auditErr error
	auditor  event.Auditor
}

// Emit an event to the auditor
func (t *testAuditor) Event(ctx context.Context, eventType string, payload interface{}) error {
	if t.auditErr != nil {
		return t.auditErr
	}
	return t.auditor.Event(ctx, eventType, payload)
}

// Specifies if the auditor is enabled or not
func (t *testAuditor) Enabled() bool {
	return t.auditor.Enabled()
}

// Reopen signals to auditor to reopen any files they have open.
func (t *testAuditor) Reopen() error {
	return t.auditor.Reopen()
}

// SetEnabled sets the auditor to enabled or disabled.
func (t *testAuditor) SetEnabled(enabled bool) {
	t.auditor.SetEnabled(enabled)
}

// DeliveryEnforced returns whether or not delivery of an audit
// log must be enforced
func (t *testAuditor) DeliveryEnforced() bool {
	return t.auditor.DeliveryEnforced()
}
