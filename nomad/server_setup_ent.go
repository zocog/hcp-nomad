//go:build ent
// +build ent

package nomad

import (
	"fmt"

	"github.com/hashicorp/sentinel/sentinel"
)

type EnterpriseState struct {
	// sentinel is a shared instance of the policy engine
	sentinel *sentinel.Sentinel

	//licenseWatcher is used to manage the lifecycle for enterprise licenses
	licenseWatcher *LicenseWatcher
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

	// Set License config options
	config.LicenseConfig = &LicenseConfig{
		AdditionalPubKeys: config.LicenseConfig.AdditionalPubKeys,
		LicenseEnvBytes:   config.LicenseEnv,
		LicensePath:       config.LicensePath,
		Logger:            s.logger,
	}

	licenseWatcher, err := NewLicenseWatcher(config.LicenseConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize enterprise licensing: %w", err)
	}
	s.EnterpriseState.licenseWatcher = licenseWatcher
	return nil
}

// startEnterpriseBackground starts the Enterprise specific workers
func (s *Server) startEnterpriseBackground() {
	// Garbage collect Sentinel policies if enabled
	if s.config.ACLEnabled {
		go s.gcSentinelPolicies(s.shutdownCh)
	}

	s.EnterpriseState.licenseWatcher.start(s.shutdownCtx)
}

func (s *Server) entVaultDelegate() *VaultEntDelegate {
	return &VaultEntDelegate{
		featureChecker: &s.EnterpriseState,
		l:              s.logger,
	}
}
