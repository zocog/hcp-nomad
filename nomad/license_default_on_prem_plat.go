// +build on_prem_platform

package nomad

import (
	"crypto/ed25519"
	"encoding/base64"
	"math/rand"
	"time"

	licensing "github.com/hashicorp/go-licensing"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
)

// defaultEnterpriseLicense returns a signed license blob and sets any
// required public key on the configuration
func defaultEnterpriseLicense(cfg *LicenseConfig) (string, error) {
	now := time.Now()
	l = &licensing.License{
		LicenseID:       permanentLicenseID,
		CustomerID:      permanentLicenseID,
		InstallationID:  "*",
		Product:         nomadLicense.ProductName,
		IssueTime:       now,
		StartTime:       now,
		ExpirationTime:  now.Add(30 * 365 * 24 * time.Hour),
		TerminationTime: now.Add(30 * 365 * 24 * time.Hour),
		Flags:           map[string]interface{}{},
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	signed, err = l.SignedString(priv)
	if err != nil {
		return "", err
	}
	pubKey = base64.StdEncoding.EncodeToString(pub)
	cfg.AdditionalPubKeys = append(cfg.AdditionalPubKeys, pubKey)

	return signed, nil
}
