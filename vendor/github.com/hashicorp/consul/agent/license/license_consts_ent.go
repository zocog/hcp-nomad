// +build consulent
// +build !consulpro
// +build !consulprem

package license

import "time"

const (
	// temporary license information
	temporaryLicenseStartOffset    = -5 * time.Minute
	temporaryServerLicenseDuration = 6 * time.Hour
	temporaryClientLicenseDuration = 30 * time.Minute
	temporaryLicensePackage        = "premium"
	temporaryLicenseID             = "temporary"

	LicenseUpdateEvent   = "consul:license-update"
	PerpetualTempLicense = false
)
