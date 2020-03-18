// +build ent

package agent

import (
	"context"
	"net/http"
	"time"

	"github.com/hashicorp/nomad/audit"
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

type AuditResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func NewAuditResponseWriter(w http.ResponseWriter) *AuditResponseWriter {
	return &AuditResponseWriter{w, http.StatusOK}
}

func (a AuditResponseWriter) WriteHeader(code int) {
	a.statusCode = code
	a.ResponseWriter.WriteHeader(code)
}

func (s *HTTPServer) eventFromReq(req *http.Request, auth *audit.Auth) *audit.Event {
	return &audit.Event{
		Stage:     audit.OperationReceived,
		Type:      audit.AuditEvent,
		Timestamp: time.Now(),
		Version:   0,
		Auth:      auth,
		Request: &audit.Request{
			Operation: req.Method,
			Endpoint:  req.URL.String(),
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

func (s *HTTPServer) auditHandler(handler handlerFn) handlerFn {
	f := func(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
		// Fast path if eventer is disabled
		if !s.agent.eventer.Enabled() {
			return handler(resp, req)
		}

		// Create a writer that captures response code
		rw := NewAuditResponseWriter(resp)

		// get token info
		var authToken string
		s.parseToken(req, &authToken)
		var err error
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

		event := s.eventFromReq(req, auth)
		err = s.agent.eventer.Event(context.Background(), "audit", event)
		if err != nil {
			// Error sending event, circumvent handler
			return nil, err
		}

		obj, err := handler(rw, req)
		// Set status code from handler potentially writing it
		event.Response = &audit.Response{}
		event.Response.StatusCode = rw.statusCode

		code, errMsg := errCodeFromHandler(err)
		if errMsg != "" {
			event.Response.Error = errMsg
		} else if code != 0 {
			event.Response.StatusCode = code
		}

		event.Stage = audit.OperationComplete
		err = s.agent.eventer.Event(context.Background(), "audit", event)
		if err != nil {
			return nil, err
		}

		return obj, nil
	}
	return f
}

func (s *HTTPServer) auditByteHandler(handler handlerByteFn) handlerByteFn {
	f := func(resp http.ResponseWriter, req *http.Request) ([]byte, error) {
		// Fast path if eventer is disabled
		if !s.agent.eventer.Enabled() {
			return handler(resp, req)
		}

		// Create a writer that captures response code
		rw := NewAuditResponseWriter(resp)

		// get token info
		var authToken string
		s.parseToken(req, &authToken)
		var err error
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

		event := s.eventFromReq(req, auth)
		err = s.agent.eventer.Event(context.Background(), "audit", event)
		if err != nil {
			// Error sending event, circumvent handler
			return nil, err
		}

		// invoke wrapped handler
		obj, err := handler(rw, req)

		// Set status code from handler potentially writing it
		event.Response = &audit.Response{}
		event.Response.StatusCode = rw.statusCode

		code, errMsg := errCodeFromHandler(err)
		if errMsg != "" {
			event.Response.Error = errMsg
		} else if code != 0 {
			event.Response.StatusCode = code
		}

		event.Stage = audit.OperationComplete
		err = s.agent.eventer.Event(context.Background(), "audit", event)
		if err != nil {
			return nil, err
		}

		return obj, nil
	}
	return f
}
