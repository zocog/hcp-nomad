// +build ent
// +build !on_prem_modules
// +build !on_prem_platform

package nomad

import (
	"github.com/hashicorp/go-licensing"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
)

func temporaryFlags() map[string]interface{} {
	return map[string]interface{}{
		"modules": []string{
			nomadLicense.ModuleGovernancePolicy.String(),
			nomadLicense.ModuleMulticlusterAndEfficiency.String(),
		},
		"temporary": true,
	}
}

func temporaryLicenseInfo() (license *licensing.License, signed, pubkey string, err error) {
	return licensing.TemporaryLicenseInfo(nomadLicense.ProductName, temporaryFlags(), temporaryLicenseTimeLimit)
}
