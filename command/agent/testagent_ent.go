//go:build ent
// +build ent

package agent

import (
	"encoding/base64"

	nomadLicense "github.com/hashicorp/nomad-licensing/license"
)

const (
	// EnterpriseTestAgent is used to configure a TestAgent's Enterprise flag
	EnterpriseTestAgent = true
)

// defaultEnterpriseTestServerConfig updates the configuration with a valid
// test license that lasts longer than any test run. This gets called early in
// the server setup so you can still override the license for a given test.
func defaultEnterpriseTestServerConfig(c *ServerConfig) {
	if c == nil {
		return
	}

	l := nomadLicense.NewTestLicense(map[string]interface{}{
		"modules": []interface{}{
			nomadLicense.ModuleGovernancePolicy.String(),
			nomadLicense.ModuleMulticlusterAndEfficiency.String(),
		},
	})

	c.LicenseEnv = l.Signed
	c.licenseAdditionalPublicKeys = []string{
		base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)}
}
