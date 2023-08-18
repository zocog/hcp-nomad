//go:build ent

package fingerprint

import (
	"github.com/hashicorp/nomad/nomad/structs/config"
)

// vaultConfigs returns the set of Vault configurations the fingerprint needs to
// check. In Nomad CE we only check the default Vault.
func (f *VaultFingerprint) vaultConfigs(req *FingerprintRequest) map[string]*config.VaultConfig {
	cfgs := map[string]*config.VaultConfig{}

	for name, cfg := range req.Config.VaultConfigs {
		if cfg.IsEnabled() {
			cfgs[name] = cfg
		}
	}
	return cfgs
}
