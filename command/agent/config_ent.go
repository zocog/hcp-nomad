//go:build ent
// +build ent

package agent

import (
	"github.com/hashicorp/nomad/helper/pointer"
)

// DefaultEntConfig allows configuring enterprise only default configuration
// values.
func DefaultEntConfig() *Config {
	return &Config{
		DisableUpdateCheck: pointer.Of(true),
	}
}
