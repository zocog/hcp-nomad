package licensing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"time"
)

type TestLicense struct {
	PubKeyEncoded string
	PublicKey     ed25519.PublicKey
	PrivateKey    ed25519.PrivateKey
	License       *License
	LicenseSigned string
}

func NewTestLicense(product string, flags map[string]interface{}, expiration time.Duration) *TestLicense {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}

	now := time.Now()
	license := &License{
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

	lSigned, err := license.SignedString(priv)
	if err != nil {
		panic(err)
	}

	return &TestLicense{
		PubKeyEncoded: base64.StdEncoding.EncodeToString(pub),
		PublicKey:     pub,
		PrivateKey:    priv,
		License:       license,
		LicenseSigned: lSigned,
	}
}
