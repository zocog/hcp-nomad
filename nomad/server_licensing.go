// +build ent

package nomad

import (
	"errors"
	"fmt"
	"time"

	version "github.com/hashicorp/go-version"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/structs"
	"golang.org/x/time/rate"
)

func (s *Server) licenseMonitor() {
	limiter := rate.NewLimiter(rate.Limit(1), 1)

	var lastLic string = ""
	for {
		if err := limiter.Wait(s.shutdownCtx); err != nil {
			return
		}

		update, lic, err := s.waitForLicenseUpdate(lastLic)
		if err != nil {
			s.logger.Warn("License sync error (will retry)", "error", err)
			select {
			case <-s.shutdownCh:
				return
			default:
			}
		}

		if update {
			_, err := s.licenseWatcher.SetLicense(lic.Signed)
			if err != nil {
				s.logger.Error("failed to set license from update", "error", err)
			} else {
				lastLic = lic.Signed
			}
		}
	}
}

func (s *Server) waitForLicenseUpdate(lastSigned string) (bool, *structs.StoredLicense, error) {
	state := s.fsm.State()
	ws := state.NewWatchSet()
	ws.Add(s.shutdownCh)

	// Perform initial query
	lic, err := state.License(ws)
	if err != nil {
		return false, nil, err
	}

	if lic != nil && lic.Signed != lastSigned {
		return true, lic, nil
	}

	// Wait for trigger
	ws.Watch(nil)

	updateLic, err := s.fsm.State().License(ws)
	if updateLic != nil && updateLic.Signed != lastSigned {
		return true, updateLic, err
	} else if updateLic == nil && lic != nil {
		return true, nil, err
	} else {
		return false, nil, err
	}
}

func (s *Server) propagateLicense(lic *license.License, signedBlob string) error {
	stored, err := s.fsm.State().License(nil)
	if err != nil {
		return err
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

func (s *Server) establishTemporaryLicenseMetadata() (int64, error) {
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
