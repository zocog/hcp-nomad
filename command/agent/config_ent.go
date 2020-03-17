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
		// handle case where rotate duration is not set
		if sink.RotateDurationHCL == "" {
			sink.RotateDuration = 0
			return nil
		}

		d, err := time.ParseDuration(sink.RotateDurationHCL)
		if err != nil {
			return err
		}
		sink.RotateDuration = d
	}

	return nil
}
