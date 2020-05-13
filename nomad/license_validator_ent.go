// +build ent

package nomad

import (
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
)

func temporaryFlags() map[string]interface{} {
	return map[string]interface{}{
		"modules": []string{nomadLicense.ModuleGovernancePolicy.String()},
	}
}
