//go:build ent
// +build ent

package agent

import (
	"github.com/hashicorp/nomad/helper/pointer"
	"github.com/hashicorp/nomad/nomad/structs/config"
)

// DefaultEntConfig allows configuring enterprise only default configuration
// values.
func DefaultEntConfig() *Config {
	return &Config{
		DisableUpdateCheck: pointer.Of(true),
		Reporting: &config.ReportingConfig{
			License: &config.LicenseReportingConfig{
				Enabled: pointer.Of(true),
			},
		},
	}
}
