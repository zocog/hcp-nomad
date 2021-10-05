//go:build ent
// +build ent

package nomad

import (
	"fmt"

	"github.com/hashicorp/consul/agent/consul/autopilot"
	"github.com/hashicorp/consul/agent/consul/autopilot_ent"
	consulLicense "github.com/hashicorp/consul/agent/license"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/raft"
)

// AdvancedAutopilotDelegate defines a policy for promoting non-voting servers in a way
// that maintains an odd-numbered voter count while respecting configured redundancy
// zones, servers marked non-voter, and any upgrade migrations to perform.
type AdvancedAutopilotDelegate struct {
	AutopilotDelegate

	promoter *autopilot_ent.AdvancedPromoter
}

func (d *AdvancedAutopilotDelegate) PromoteNonVoters(conf *autopilot.Config, health autopilot.OperatorHealthReply) ([]raft.Server, error) {
	minRaftProtocol, err := d.server.autopilot.MinRaftProtocol()
	if err != nil {
		return nil, fmt.Errorf("error getting server raft protocol versions: %s", err)
	}

	// If we don't meet the minimum version for non-voter features, bail early
	if minRaftProtocol < 3 {
		return nil, nil
	}

	return d.promoter.PromoteNonVoters(conf, health)
}

// getNodeMeta tries to fetch a node's metadata
func (s *Server) getNodeMeta(serverID raft.ServerID) (map[string]string, error) {
	meta := make(map[string]string)
	for _, member := range s.Members() {
		ok, parts := isNomadServer(member)
		if !ok || raft.ServerID(parts.ID) != serverID {
			continue
		}

		meta[AutopilotRZTag] = member.Tags[AutopilotRZTag]
		meta[AutopilotVersionTag] = member.Tags[AutopilotVersionTag]
		break
	}

	return meta, nil
}

// ConsulFeatureCheck no-ops consul enterprise license feature checking functionality
// TODO remove private consul dependencies
func (s *Server) ConsulFeatureCheck(feature consulLicense.Features, allowPrevious, emitLog bool) error {
	// Hack to convert consul license feature to nomad license feature
	nomadFeature, err := license.FeatureFromString(feature.String())
	if err != nil {
		return err
	}
	return s.EnterpriseState.FeatureCheck(nomadFeature, true)
}

// Set up the enterprise version of autopilot
func (s *Server) setupEnterpriseAutopilot(config *Config) {
	apDelegate := &AdvancedAutopilotDelegate{
		AutopilotDelegate: AutopilotDelegate{server: s},
	}
	// TODO:(Consul) Consul's autopilot package is a private package. Work
	// with consul to create a module for it.
	apDelegate.promoter = autopilot_ent.NewAdvancedPromoter(s.logger, apDelegate, s.getNodeMeta, s.ConsulFeatureCheck)
	s.autopilot = autopilot.NewAutopilot(s.logger, apDelegate, config.AutopilotInterval, config.ServerHealthInterval)
}
