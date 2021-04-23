// +build ent

package nomad

import (
	"fmt"

	"github.com/hashicorp/nomad-licensing/license"
)

// FeatureCheck checks if a requested feature is licensed or not
func (es *EnterpriseState) FeatureCheck(feature license.Features, emitLog bool) error {
	if es.licenseWatcher == nil {
		return fmt.Errorf("license not initialized")
	}

	return es.licenseWatcher.FeatureCheck(feature, emitLog)
}

// SetLicense sets the server license if the supplied license blob is valid
// and unexpired
func (es *EnterpriseState) SetLicense(blob string) error {
	if es.licenseWatcher == nil {
		return fmt.Errorf("license watcher unable to set license")
	}

	return es.licenseWatcher.SetLicense(blob)
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

// FileLicense returns the file license used to initialize the
// server. It may not be set or may not be the license currently in use.
func (es *EnterpriseState) FileLicenseOutdated() bool {
	if es.licenseWatcher == nil {
		// everything is licensed while the watcher starts up
		return false
	}

	fileLic := es.licenseWatcher.FileLicense()
	if fileLic == "" {
		return false
	}

	currentBlob := es.licenseWatcher.LicenseBlob()
	return fileLic != currentBlob
}

// ReloadLicense reloads the server's file license if the supplied license cfg
// is valid and unexpired
func (es *EnterpriseState) ReloadLicense(cfg *Config) error {
	if es.licenseWatcher == nil {
		return nil
	}
	licenseConfig := &LicenseConfig{
		LicenseEnvBytes: cfg.LicenseEnv,
		LicensePath:     cfg.LicensePath,
	}
	return es.licenseWatcher.Reload(licenseConfig)
}
