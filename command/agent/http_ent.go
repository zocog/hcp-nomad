// +build ent

package agent

import "net/http"

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

func (s HTTPServer) auditHandler(h handlerFn) handlerFn {
	return h
}

func (s *HTTPServer) auditByteHandler(h handlerByteFn) handlerByteFn {
	return h
}

func (s *HTTPServer) auditHTTPHandler(h http.Handler) http.Handler {
	return h
}
