// +build ent
// +build !on_prem_modules
// +build !on_prem_platform

package nomad

// defaultEnterpriseLicense returns a signed license blob and sets any
// required public key on the configuration
func defaultEnterpriseLicense(cfg *LicenseConfig) (string, error) { return "", nil }
