//go:build ent
// +build ent

package nomad

import "net/rpc"

// EnterpriseEndpoints holds the set of enterprise only endpoints to register
type EnterpriseEndpoints struct {
	Quota          *Quota
	Sentinel       *Sentinel
	License        *License
	Recommendation *Recommendation
}

// NewEnterpriseEndpoints returns the set of Nomad Enterprise only endpoints.
func NewEnterpriseEndpoints(s *Server, ctx *RPCContext) *EnterpriseEndpoints {
	return &EnterpriseEndpoints{
		Quota:          NewQuotaEndpoint(s, ctx),
		Sentinel:       NewSentinelEndpoint(s, ctx),
		License:        NewLicenseEndpoint(s, ctx),
		Recommendation: NewRecommendationEndpoint(s, ctx),
	}
}

// Register register the enterprise endpoints.
func (e *EnterpriseEndpoints) Register(rpcServer *rpc.Server) {
	rpcServer.Register(e.Quota)
	rpcServer.Register(e.Sentinel)
	rpcServer.Register(e.License)
	rpcServer.Register(e.Recommendation)
}
