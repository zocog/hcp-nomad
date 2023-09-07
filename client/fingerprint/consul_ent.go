//go:build ent

package fingerprint

import "github.com/hashicorp/nomad/nomad/structs/config"

// consulConfigs returns the set of Consul configurations the fingerprint needs to
// check.
func (f *ConsulFingerprint) consulConfigs(req *FingerprintRequest) map[string]*config.ConsulConfig {
	agentCfg := req.Config
	if agentCfg.ConsulConfig == nil {
		return nil
	}

	return req.Config.ConsulConfigs
}
