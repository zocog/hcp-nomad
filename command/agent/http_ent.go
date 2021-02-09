// +build ent

package agent

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	metrics "github.com/armon/go-metrics"
	"github.com/hashicorp/nomad/audit"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/structs"
)

// registerEnterpriseHandlers registers Nomad Pro and Premium endpoints
func (s *HTTPServer) registerEnterpriseHandlers() {
	s.registerEntHandlers()
}

// registerEntHandlers registers Nomad Premium endpoints
func (s *HTTPServer) registerEntHandlers() {
	s.mux.HandleFunc("/v1/sentinel/policies", s.wrap(s.SentinelPoliciesRequest))
	s.mux.HandleFunc("/v1/sentinel/policy/", s.wrap(s.SentinelPolicySpecificRequest))

	s.mux.HandleFunc("/v1/quotas", s.wrap(s.QuotasRequest))
	s.mux.HandleFunc("/v1/quota-usages", s.wrap(s.QuotaUsagesRequest))
	s.mux.HandleFunc("/v1/quota/", s.wrap(s.QuotaSpecificRequest))
	s.mux.HandleFunc("/v1/quota", s.wrap(s.QuotaCreateRequest))

	s.mux.HandleFunc("/v1/recommendation", s.wrap(s.RecommendationCreateRequest))
	s.mux.HandleFunc("/v1/recommendations", s.wrap(s.RecommendationsListRequest))
	s.mux.HandleFunc("/v1/recommendations/apply", s.wrap(s.RecommendationsApplyRequest))
	s.mux.HandleFunc("/v1/recommendation/", s.wrap(s.RecommendationSpecificRequest))
}

type auditResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newAuditResponseWriter(w http.ResponseWriter) *auditResponseWriter {
	return &auditResponseWriter{w, http.StatusOK}
}

func (a auditResponseWriter) WriteHeader(code int) {
	a.statusCode = code
	a.ResponseWriter.WriteHeader(code)
}

func (a auditResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := a.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack not supported")
	}
	return h.Hijack()
}

func (a auditResponseWriter) Flush() {
	if fl, ok := a.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

func (s *HTTPServer) eventFromReq(ctx context.Context, req *http.Request, auth *audit.Auth) *audit.Event {
	// retrieve namespace from request headers
	var namespace string
	parseNamespace(req, &namespace)

	// Get request ID
	reqIDRaw := ctx.Value(ContextKeyReqID)
	reqID, ok := reqIDRaw.(string)
	if !ok || reqID == "" {
		s.logger.Error("Failed to convert context value for request ID")
		reqID = MissingRequestID
	}

	agentCfg := s.agent.GetConfig()
	return &audit.Event{
		ID:        uuid.Generate(),
		Stage:     audit.OperationReceived,
		Type:      audit.AuditEvent,
		Timestamp: time.Now(),
		Version:   audit.SchemaV1,
		Auth:      auth,
		Request: &audit.Request{
			ID:        reqID,
			Operation: req.Method,
			Endpoint:  req.URL.String(),
			Namespace: map[string]string{
				"id": namespace,
			},
			RequestMeta: map[string]string{
				"remote_address": req.RemoteAddr,
				"user_agent":     req.UserAgent(),
			},
			NodeMeta: map[string]string{
				// Use the HTTP address since this is an HTTP-originated audit log.
				// May differ from the server's RPC/Serf IPs.
				"ip": agentCfg.AdvertiseAddrs.HTTP,
			},
		},
	}
}

func (s *HTTPServer) auditRequest(ctx context.Context, req *http.Request) (*audit.Event, error) {
	// get token info
	var authToken string
	s.parseToken(req, &authToken)
	var err error
	// TODO OPTIMIZATION:
	// Look into adding this to the request context so it can be used
	// in the invoked handlers
	var token *structs.ACLToken

	if srv := s.agent.Server(); srv != nil {
		token, err = srv.ResolveSecretToken(authToken)
	} else {
		// Not a Server; use the Client for token resolution
		token, err = s.agent.Client().ResolveSecretToken(authToken)
	}
	if err != nil {
		// If error is no token create an empty one
		if err == structs.ErrTokenNotFound {
			token = &structs.ACLToken{}
		} else {
			s.logger.Error("retrieving acl token from request", "err", err)
			return nil, err
		}
	}
	// Prevent nil token with anonymous one
	if token == nil {
		token = structs.AnonymousACLToken
	}

	auth := &audit.Auth{
		AccessorID: token.AccessorID,
		Name:       token.Name,
		Policies:   token.Policies,
		Global:     token.Global,
		CreateTime: token.CreateTime,
	}

	event := s.eventFromReq(ctx, req, auth)
	err = s.agent.auditor.Event(ctx, "audit", event)
	if err != nil {
		// Error sending event, circumvent handler
		return nil, err
	}

	return event, nil
}

func (s *HTTPServer) auditHTTPHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Fast-Path if disabled
		if s.agent.auditor == nil || !s.agent.auditor.Enabled() {
			h.ServeHTTP(w, req)
			return
		}
		defer metrics.MeasureSince([]string{"http", "audit", "http_handler"}, time.Now())

		ctx := req.Context()
		reqID := uuid.Generate()
		ctx = context.WithValue(ctx, ContextKeyReqID, reqID)
		req = req.WithContext(ctx)

		// Create a writer that captures response code
		rw := newAuditResponseWriter(w)

		event, err := s.auditRequest(ctx, req)
		if err != nil {
			s.logger.Error("error creating audit entry from request", "err", err, "request_id", reqID)
			// Error sending event, circumvent handler
			return
		}

		// Invoke wrapped handler
		h.ServeHTTP(rw, req)

		err = s.auditResp(ctx, rw, event, nil)
		if err != nil {
			s.logger.Error("auditing response from HTTPHandler", "err", err, "request_id", reqID)
			// handle this error case (write new response body?)
			return
		}
	})
}

func (s *HTTPServer) auditResp(ctx context.Context, rw *auditResponseWriter, e *audit.Event, respErr error) error {
	// Set status code from handler potentially writing it
	e.Response = &audit.Response{}
	e.Response.StatusCode = rw.statusCode

	code, errMsg := errCodeFromHandler(respErr)
	if errMsg != "" {
		e.Response.Error = errMsg
	}
	if code != 0 {
		e.Response.StatusCode = code
	}

	e.Stage = audit.OperationComplete
	err := s.agent.auditor.Event(ctx, "audit", e)
	if err != nil {
		return err
	}
	return nil
}

func (s *HTTPServer) auditHandler(handler handlerFn) handlerFn {
	f := func(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
		// Set context request ID
		ctx := req.Context()
		reqID := uuid.Generate()
		ctx = context.WithValue(ctx, ContextKeyReqID, reqID)
		req = req.WithContext(ctx)

		// Fast path if eventer is disabled
		if s.agent.auditor == nil || !s.agent.auditor.Enabled() {
			return handler(resp, req)
		}
		defer metrics.MeasureSince([]string{"http", "audit", "handler"}, time.Now())

		// Create a writer that captures response code
		rw := newAuditResponseWriter(resp)

		// Create audit event from request
		event, err := s.auditRequest(ctx, req)
		if err != nil {
			// Error sending event, circumvent handler
			s.logger.Error("failed to audit log request, blocking response", "err", err, "request_id", reqID)
			return nil, err
		}

		// Invoke wrapped handler
		obj, rspErr := handler(rw, req)

		err = s.auditResp(ctx, rw, event, rspErr)
		if err != nil {
			s.logger.Error("failed to audit log response, blocking response", "err", err, "request_id", reqID)
			return nil, err
		}

		return obj, rspErr
	}
	return f
}

func (s *HTTPServer) auditNonJSONHandler(handler handlerByteFn) handlerByteFn {
	f := func(resp http.ResponseWriter, req *http.Request) ([]byte, error) {
		// Set context request ID
		ctx := req.Context()
		reqID := uuid.Generate()
		ctx = context.WithValue(ctx, ContextKeyReqID, reqID)
		req = req.WithContext(ctx)

		// Fast path if eventer is disabled
		if s.agent.auditor == nil || !s.agent.auditor.Enabled() {
			return handler(resp, req)
		}
		defer metrics.MeasureSince([]string{"http", "audit", "non_json_handler"}, time.Now())

		// Create a writer that captures response code
		rw := newAuditResponseWriter(resp)

		// Create audit event from request
		event, err := s.auditRequest(ctx, req)
		if err != nil {
			// Error sending event, circumvent handler
			s.logger.Error("failed to audit log request, blocking response", "err", err, "request_id", reqID)
			return nil, err
		}

		// Invoke wrapped handler
		obj, rspErr := handler(rw, req)

		err = s.auditResp(ctx, rw, event, rspErr)
		if err != nil {
			s.logger.Error("failed to audit log response, blocking response", "err", err, "request_id", reqID)
			return nil, err
		}

		return obj, rspErr
	}
	return f
}
