//go:build ent
// +build ent

package nomad

import (
	"encoding/base64"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	licensing "github.com/hashicorp/go-licensing"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
)

// defaultEnterpriseTestConfig updates the configuration with a valid test
// license that lasts longer than any test run. This gets called early in
// nomad.TestServer so you can still override the license for a given test.
func defaultEnterpriseTestConfig(c *Config) {
	c.LicenseEnv = defaultTestLicense()
	c.LicenseConfig = &LicenseConfig{
		AdditionalPubKeys: encodedTestLicensePubKeys(),
		Logger:            hclog.NewInterceptLogger(nil),
	}
}

// defaultTestLicense returns a license signature for a license that has no
// feature flags and will last long enough to cover the expected test run time
func defaultTestLicense() string {
	return testLicense(
		"test-license",
		time.Now().Add(-1*time.Minute),
		time.Now().Add(30*time.Minute))
}

// licenseForGovernance is a test helper that returns license
// that's valid for the "Governance & Policy" module.
func licenseForGovernance() *nomadLicense.TestLicense {
	return nomadLicense.NewTestLicense(map[string]interface{}{
		"modules": []interface{}{
			nomadLicense.ModuleGovernancePolicy.String(),
		},
	})
}

// licenseForMulticlusterEfficiency is a test helper that returns license
// that's valid for the "Multicluster & Efficiency" module.
func licenseForMulticlusterEfficiency() *nomadLicense.TestLicense {
	return nomadLicense.NewTestLicense(map[string]interface{}{
		"modules": []interface{}{
			nomadLicense.ModuleMulticlusterAndEfficiency.String(),
		},
	})
}

// testLicense generates a license signature for the given issue and
// expiration time. It has no features, so use nomadLicense.NewTestLicense
// for tests that require feature checks.
func testLicense(id string, issue, exp time.Time) string {
	l := &licensing.License{
		LicenseID:       id,
		CustomerID:      "test customer id",
		InstallationID:  "*",
		Product:         "nomad",
		IssueTime:       issue,
		StartTime:       issue,
		ExpirationTime:  exp,
		TerminationTime: exp,
		Flags:           map[string]interface{}{},
	}
	signed, _ := l.SignedString(nomadLicense.TestPrivateKey)
	return signed
}

func encodedTestLicensePubKeys() []string {
	return []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)}
}
