// +build on_prem_modules

package nomad

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/hashicorp/go-licensing"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
)

func temporaryFlags() map[string]interface{} {
	return map[string]interface{}{
		"modules": []string{
			nomadLicense.ModuleGovernancePolicy.String(),
			nomadLicense.ModuleMulticlusterAndEfficiency.String(),
		},
	}
}

func temporaryLicenseInfo() (license *licensing.License, signed, pubkey string, err error) {
	now := time.Now()
	l := &licensing.License{
		LicenseID:      permanentLicenseID,
		CustomerID:     permanentLicenseID,
		InstallationID: "*",
		Product:        nomadLicense.ProductName,
		IssueTime:      now,
		StartTime:      now,
		ExpirationTime: 30 * 365 * 24 * time.Hour,
		ExpirationTime: 30 * 365 * 24 * time.Hour,
		Flags:          temporaryFlags(),
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", "", err
	}
	signed, err = l.SignedString(priv)
	if err != nil {
		return nil, "", "", err
	}
	pubKey = base64.StdEncoding.EncodeToString(pub)
	return l, signed, pubKey, nil
}
