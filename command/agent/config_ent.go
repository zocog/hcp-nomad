// +build pro ent

package agent

import (
	"time"

	"github.com/hashicorp/nomad/helper"
)

// DefaultEntConfig allows configuring enterprise only default configuration
// values.
func DefaultEntConfig() *Config {
	return &Config{
		DisableUpdateCheck: helper.BoolToPtr(true),
	}
}

func (c *Config) entParseConfig() error {
	// convert sink durations to time.Durations
	for _, sink := range c.Audit.Sinks {
		d, err := time.ParseDuration(sink.RotateDurationHCL)
		if err != nil {
			return err
		}
		sink.RotateDuration = d
	}

	return nil
}
