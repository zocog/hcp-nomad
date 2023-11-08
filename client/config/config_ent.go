//go:build ent

package config

import (
	"github.com/hashicorp/go-hclog"
	structsc "github.com/hashicorp/nomad/nomad/structs/config"
)

// GetVaultConfigs returns the set of Vault configurations available for this
// client. In Nomad CE we only use the default Vault.
func (c *Config) GetVaultConfigs(logger hclog.Logger) map[string]*structsc.VaultConfig {
	cfgs := map[string]*structsc.VaultConfig{}

	for name, cfg := range c.VaultConfigs {
		if cfg.IsEnabled() {
			cfgs[name] = cfg
		}
	}
	return cfgs
}

// GetConsulConfigs returns the set of Consul configurations available for this
// client. In Nomad CE we only use the default Consul.
func (c *Config) GetConsulConfigs(logger hclog.Logger) map[string]*structsc.ConsulConfig {
	if c.ConsulConfigs == nil {
		return nil
	}

	return c.ConsulConfigs
}
