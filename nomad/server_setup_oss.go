// +build !pro,!ent

package nomad

import (
	"fmt"

	"github.com/hashicorp/consul/agent/consul/autopilot"
)

type EnterpriseState struct{}

func (es *EnterpriseState) FeatureCheckPreemption() error {
	return fmt.Errorf("Feature \"Preemption\" is unlicensed")
}

func (s *Server) setupEnterprise(config *Config) error {
	// Set up the OSS version of autopilot
	apDelegate := &AutopilotDelegate{s}
	s.autopilot = autopilot.NewAutopilot(s.logger, apDelegate, config.AutopilotInterval, config.ServerHealthInterval)

	return nil
}

func (s *Server) startEnterpriseBackground() {}
