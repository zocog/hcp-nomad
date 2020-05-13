package licensing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"time"
)

// TemporaryLicense takes minimal configuration to return a v1-signed license string and
// the hex-encoded public key to be used for product initialization
func TemporaryLicense(product string, flags map[string]interface{}, expiration time.Duration) (signedLicense string, pubKey string, err error) {
	_, signedLicense, pubKey, err = TemporaryLicenseInfo(product, flags, expiration)
	if err != nil {
		return "", "", err
	}
	return signedLicense, pubKey, nil
}

func TemporaryLicenseInfo(product string, flags map[string]interface{}, expiration time.Duration) (license *License, signed, pubKey string, err error) {
	now := time.Now()
	license = &License{
		LicenseID:       "temporary-license",
		CustomerID:      "temporary license customer",
		InstallationID:  "*",
		Product:         product,
		IssueTime:       now,
		StartTime:       now,
		ExpirationTime:  now.Add(expiration),
		TerminationTime: now.Add(expiration),
		Flags:           flags,
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", "", err
	}
	signed, err = license.SignedString(priv)
	if err != nil {
		return nil, "", "", err
	}
	pubKey = base64.StdEncoding.EncodeToString(pub)
	return license, signed, pubKey, nil
}
