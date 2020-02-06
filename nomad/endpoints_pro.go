// +build pro

package nomad

import "net/rpc"

// EnterpriseEndpoints holds the set of enterprise only endpoints to register
type EnterpriseEndpoints struct {
}

// NewEnterpriseEndpoints returns the set of Nomad Pro only endpoints.
func NewEnterpriseEndpoints(s *Server) *EnterpriseEndpoints {
	return &EnterpriseEndpoints{}
}

// Register register the enterprise endpoints.
func (e *EnterpriseEndpoints) Register(rpcServer *rpc.Server) {}
