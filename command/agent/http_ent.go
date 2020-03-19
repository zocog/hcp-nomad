// +build ent

package agent

import (
	"context"
	"net/http"
	"time"

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

	s.mux.HandleFunc("/v1/namespaces", s.wrap(s.NamespacesRequest))
	s.mux.HandleFunc("/v1/namespace", s.wrap(s.NamespaceCreateRequest))
	s.mux.HandleFunc("/v1/namespace/", s.wrap(s.NamespaceSpecificRequest))
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

func (s *HTTPServer) eventFromReq(ctx context.Context, req *http.Request, auth *audit.Auth) *audit.Event {
	var namespace string
	parseNamespace(req, &namespace)

	// Get request ID
	reqIDRaw := ctx.Value(ContextKeyReqID)
	reqID, ok := reqIDRaw.(string)
	if !ok {
		s.logger.Error("Failed to convert context value for request ID")
		reqID = MissingRequestID
	}

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
				"ip": s.Addr,
			},
		},
	}
}

func (s *HTTPServer) auditHTTPHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		reqID := uuid.Generate()
		ctx = context.WithValue(ctx, ContextKeyReqID, reqID)
		req = req.WithContext(ctx)

		// Create a writer that captures response code
		rw := newAuditResponseWriter(w)

		event, err := s.auditReq(ctx, req)
		if err != nil {
			// Error sending event, circumvent handler
			return
		}

		// Invoke wrapped handler
		h.ServeHTTP(rw, req)

		err = s.auditResp(ctx, rw, event, nil)
		if err != nil {
			s.logger.Error("Error auditing response from HTTPHandler", "err", err.Error())
			// handle this error case (write new response body?)
			return
		}
	})
}

func (s *HTTPServer) auditReq(ctx context.Context, req *http.Request) (*audit.Event, error) {
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
			return nil, err
		}
	}
	auth := &audit.Auth{
		AccessorID: token.AccessorID,
		Name:       token.Name,
		Policies:   token.Policies,
		Global:     token.Global,
		CreateTime: token.CreateTime,
	}

	event := s.eventFromReq(ctx, req, auth)
	err = s.agent.eventer.Event(ctx, "audit", event)
	if err != nil {
		// Error sending event, circumvent handler
		return nil, err
	}

	return event, nil
}

func (s *HTTPServer) auditResp(ctx context.Context, rw *auditResponseWriter, e *audit.Event, respErr error) error {
	// Set status code from handler potentially writing it
	e.Response = &audit.Response{}
	e.Response.StatusCode = rw.statusCode

	code, errMsg := errCodeFromHandler(respErr)
	if errMsg != "" {
		e.Response.Error = errMsg
	} else if code != 0 {
		e.Response.StatusCode = code
	}

	e.Stage = audit.OperationComplete
	err := s.agent.eventer.Event(ctx, "audit", e)
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
		if s.agent.eventer == nil || !s.agent.eventer.Enabled() {
			return handler(resp, req)
		}

		// Create a writer that captures response code
		rw := newAuditResponseWriter(resp)

		event, err := s.auditReq(ctx, req)
		if err != nil {
			// Error sending event, circumvent handler
			return nil, err
		}

		// Invoke wrapped handler
		obj, rspErr := handler(rw, req)

		err = s.auditResp(ctx, rw, event, rspErr)
		if err != nil {
			return nil, err
		}

		return obj, rspErr
	}
	return f
}

func (s *HTTPServer) auditByteHandler(handler handlerByteFn) handlerByteFn {
	f := func(resp http.ResponseWriter, req *http.Request) ([]byte, error) {
		// Set context request ID
		ctx := req.Context()
		reqID := uuid.Generate()
		ctx = context.WithValue(ctx, ContextKeyReqID, reqID)
		req = req.WithContext(ctx)

		// Fast path if eventer is disabled
		if !s.agent.eventer.Enabled() {
			return handler(resp, req)
		}

		// Create a writer that captures response code
		rw := newAuditResponseWriter(resp)

		event, err := s.auditReq(ctx, req)
		if err != nil {
			// Error sending event, circumvent handler
			return nil, err
		}

		// Invoke wrapped handler
		obj, rspErr := handler(rw, req)

		err = s.auditResp(ctx, rw, event, rspErr)
		if err != nil {
			return nil, err
		}

		return obj, rspErr
	}
	return f
}
