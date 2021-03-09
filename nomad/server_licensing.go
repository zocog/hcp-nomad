// +build ent

package nomad

import (
	"errors"
	"fmt"
	"time"

	version "github.com/hashicorp/go-version"
	"github.com/hashicorp/nomad-licensing/license"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/structs"
)

func (es *EnterpriseState) FeatureCheck(feature license.Features, emitLog bool) error {
	if es.licenseWatcher == nil {
		// everything is licensed while the watcher starts up
		return nil
	}

	return es.licenseWatcher.FeatureCheck(feature, emitLog)
}

func (es *EnterpriseState) SetLicense(blob string, force bool) error {
	if es.licenseWatcher == nil {
		return fmt.Errorf("license watcher unable to set license")
	}

	return es.licenseWatcher.SetLicense(blob, force)
}

func (es *EnterpriseState) Features() uint64 {
	return uint64(es.licenseWatcher.Features())
}

// License returns the current license
func (es *EnterpriseState) License() *license.License {
	if es.licenseWatcher == nil {
		// everything is licensed while the watcher starts up
		return nil
	}
	return es.licenseWatcher.License()
}

func (es *EnterpriseState) ReloadLicense(cfg *Config) error {
	if es.licenseWatcher == nil {
		return nil
	}
	licenseConfig := &LicenseConfig{
		LicenseEnvBytes: cfg.LicenseFileEnv,
		LicensePath:     cfg.LicenseFilePath,
	}
	return es.licenseWatcher.Reload(licenseConfig)
}

func (s *Server) propagateLicense(lic *nomadLicense.License, signedBlob string) error {
	stored, err := s.fsm.State().License(nil)
	if err != nil {
		return err
	}

	if lic.Temporary {
		s.logger.Debug("license is temporary, not propagating to raft")
		return nil
	}

	if !s.IsLeader() {
		s.logger.Debug("server is not leader, not propagating to raft")
		return nil
	}

	if stored == nil || stored.Signed != signedBlob {
		newLicense := structs.StoredLicense{Signed: signedBlob}
		req := structs.LicenseUpsertRequest{License: &newLicense}
		if _, _, err := s.raftApply(structs.LicenseUpsertRequestType, &req); err != nil {
			return err
		}
	}
	return nil
}

var minLicenseMetaVersion = version.Must(version.NewVersion("0.12.1"))

func (s *Server) initTmpLicenseBarrier() (int64, error) {
	if !ServersMeetMinimumVersion(s.Members(), minLicenseMetaVersion, false) {
		s.logger.Named("core").Debug("cannot initialize temporary license barrier until all servers are above minimum version", "min_version", minLicenseMetaVersion)
		return 0, fmt.Errorf("temporary license barrier cannot be created until all servers are above minimum version %s", minLicenseMetaVersion)
	}

	fsmState := s.fsm.State()
	existingMeta, err := fsmState.TmpLicenseBarrier(nil)
	if err != nil {
		s.logger.Named("core").Error("failed to get temporary license barrier", "error", err)
		return 0, err
	}

	// If tmp license barrier already exists nothing to do
	if existingMeta != nil {
		return existingMeta.CreateTime, nil
	}

	if !s.IsLeader() {
		return 0, errors.New("server is not current leader, cannot create temporary license barrier")
	}

	// Apply temporary license timestamp
	timestamp := time.Now().UnixNano()
	req := structs.TmpLicenseBarrier{CreateTime: timestamp}
	if _, _, err := s.raftApply(structs.TmpLicenseUpsertRequestType, req); err != nil {
		s.logger.Error("failed to initialize temporary license barrier", "error", err)
		return 0, err
	}
	return timestamp, nil
}
