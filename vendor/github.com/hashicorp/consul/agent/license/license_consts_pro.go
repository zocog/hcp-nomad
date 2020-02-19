// +build consulent
// +build consulpro

package license

import "time"

const (
	// temporary license information
	temporaryLicenseStartOffset    = -5 * time.Minute
	temporaryServerLicenseDuration = 30 * 365 * 24 * time.Hour
	temporaryClientLicenseDuration = 30 * 365 * 24 * time.Hour
	temporaryLicensePackage        = "pro"
	temporaryLicenseID             = "permanent"

	LicenseUpdateEvent   = "consul:license-update"
	PerpetualTempLicense = true
)
