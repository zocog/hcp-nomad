//go:build ent
// +build ent

package nomad

import (
	"fmt"

	"github.com/hashicorp/nomad-licensing/license"
	autopilot "github.com/hashicorp/raft-autopilot"
	autopilotent "github.com/hashicorp/raft-autopilot-enterprise"

	"github.com/hashicorp/nomad/nomad/structs"
)

// AutopilotEntDelegate is a Nomad delegate for Enterprise autopilot
// operations. It implements the autopilotent.ApplicationIntegration interface
// and methods required for that have been documented as such below.
//
// This defines a policy for promoting non-voting servers in a way that
// maintains an odd-numbered voter count while respecting configured redundancy
// zones, servers marked non-voter, and any upgrade migrations to perform.
type AutopilotEntDelegate struct {
	srv *Server
}

func (d *AutopilotEntDelegate) LicenseCheck(feature autopilotent.LicensedFeature, allowPrevious bool, emitLog bool) error {

	var nomadFeature license.Features
	switch feature {
	case autopilotent.FeatureRedundancyZones:
		nomadFeature = license.FeatureRedundancyZones
	case autopilotent.FeatureReadReplicas:
		nomadFeature = license.FeatureReadScalability
	case autopilotent.FeatureAutoUpgrades:
		nomadFeature = license.FeatureAutoUpgrades
	default:
		return fmt.Errorf("the autopilot feature %s is unknown", feature)
	}
	return d.srv.EnterpriseState.FeatureCheck(nomadFeature, emitLog)
}

func (s *Server) autopilotPromoter() autopilot.Promoter {
	p, err := autopilotent.NewPromoter(
		&AutopilotEntDelegate{srv: s},
		autopilotent.WithLogger(s.logger),
	)
	if err != nil {
		s.logger.Error("Failed to create the enterprise autopilot promoter", "error", err)
		return autopilot.DefaultPromoter()
	}
	return p
}

// autopilotConfigExt returns the autopilotent.Config extensions needed for ENT
// feature support
func autopilotConfigExt(c *structs.AutopilotConfig) interface{} {

	conf := &autopilotent.Config{
		DisableUpgradeMigration: c.DisableUpgradeMigration,
	}

	if c.EnableRedundancyZones {
		conf.RedundancyZoneTag = AutopilotRZTag
	}
	if c.EnableCustomUpgrades {
		conf.UpgradeVersionTag = AutopilotVersionTag
	}

	return conf
}

func (s *Server) autopilotServerExt(parts *serverParts) interface{} {
	// Most of the enterprise specific autopilot features are pulled later when
	// Autopilot calls out to the enterprise promoters GetServerExt function.
	// However because marking a server as a read replica comes through a serf
	// tag instead of in Node metadata in the catalog we have to provide it here
	// and the promoter will not overrite this value.
	return &autopilotent.Server{
		ReadReplica: parts.NonVoter,
		// The redundancy zone and auto-ugprade version will be filled in by the promoter
	}
}
