// +build ent

package nomad

import (
	"errors"
	"fmt"
	"time"

	version "github.com/hashicorp/go-version"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/sentinel/sentinel"
)

// LicenseConfig allows for tunable licensing config
// primarily used for enterprise testing
type LicenseConfig struct {
	AdditionalPubKeys []string

	// preventStart is used for testing to control when to start watcher
	preventStart bool
}

type EnterpriseState struct {
	// sentinel is a shared instance of the policy engine
	sentinel *sentinel.Sentinel

	//licenseWatcher is used to manage the lifecycle for enterprise licenses
	licenseWatcher *LicenseWatcher
}

func (es *EnterpriseState) FeatureCheck(feature license.Features, emitLog bool) error {
	if es.licenseWatcher == nil {
		// everything is licensed while the watcher starts up
		return nil
	}

	return es.licenseWatcher.FeatureCheck(feature, emitLog)
}

func (es *EnterpriseState) Features() uint64 {
	return uint64(es.licenseWatcher.Features())
}

// setupEnterprise is used for Enterprise specific setup
func (s *Server) setupEnterprise(config *Config) error {
	// Enable the standard lib, except the HTTP import.
	stdlib := sentinel.ImportsMap(sentinel.StdImports())
	stdlib.Blacklist([]string{"http"})

	// Setup the sentinel configuration
	sentConf := &sentinel.Config{
		Imports: stdlib,
	}
	if config.SentinelConfig != nil {
		for _, sImport := range config.SentinelConfig.Imports {
			sentConf.Imports[sImport.Name] = &sentinel.Import{
				Path: sImport.Path,
				Args: sImport.Args,
			}
			s.logger.Named("sentinel").Debug("enabling imports", "name", sImport.Name, "path", sImport.Path)
		}
	}

	// Create the Sentinel instance based on the configuration
	s.sentinel = sentinel.New(sentConf)

	s.setupEnterpriseAutopilot(config)

	licenseWatcher, err := NewLicenseWatcher(s.logger, config.LicenseConfig, config.AgentShutdown, s.establishTemporaryLicenseMetadata, s.State)
	if err != nil {
		return fmt.Errorf("failed to create a new license watcher: %w", err)
	}
	s.EnterpriseState.licenseWatcher = licenseWatcher
	if !config.LicenseConfig.preventStart {
		s.EnterpriseState.licenseWatcher.start(s.shutdownCtx)
	}
	return nil
}

// startEnterpriseBackground starts the Enterprise specific workers
func (s *Server) startEnterpriseBackground() {
	// Garbage collect Sentinel policies if enabled
	if s.config.ACLEnabled {
		go s.gcSentinelPolicies(s.shutdownCh)
	}
}

var minLicenseMetaVersion = version.Must(version.NewVersion("0.12.1"))

func (s *Server) establishTemporaryLicenseMetadata() (int64, error) {
	if !ServersMeetMinimumVersion(s.Members(), minLicenseMetaVersion, false) {
		s.logger.Named("core").Debug("cannot initialize temporary license until all servers are above minimum version", "min_version", minLicenseMetaVersion)
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

func (s *Server) entVaultDelegate() *VaultEntDelegate {
	return &VaultEntDelegate{
		featureChecker: &s.EnterpriseState,
		l:              s.logger,
	}
}
