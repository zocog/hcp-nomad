// +build ent

package nomad

import (
	"errors"

	"github.com/hashicorp/go-licensing"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
)

func licenseValidator(lic *licensing.License, licenseStr string) error {
	n, err := nomadLicense.NewLicense(lic)
	if err != nil {
		return err
	}

	for _, module := range n.Modules {
		if module == nomadLicense.ModuleGovernancePolicy {
			return nil
		}
	}
	return errors.New("License did not contain proper module for enterprise build.")
}

func temporaryFlags() map[string]interface{} {
	return map[string]interface{}{
		"modules": []string{nomadLicense.ModuleGovernancePolicy.String()},
	}
}
