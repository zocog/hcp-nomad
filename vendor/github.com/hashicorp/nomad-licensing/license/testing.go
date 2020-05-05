package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"time"

	"github.com/hashicorp/go-licensing"
)

var (
	TestPublicKey  ed25519.PublicKey
	TestPrivateKey ed25519.PrivateKey
)

func init() {
	TestPublicKey, _ = base64.StdEncoding.DecodeString("K8hQGHP8/KtdLZ0tUJkKllYXCscxy6yqfpgBHTxFwu4=")
	TestPrivateKey, _ = base64.StdEncoding.DecodeString("GipwbPnbVtTZ+/GHuzdXvlPDUjw/HCq3zTygy6atqT0ryFAYc/z8q10tnS1QmQqWVhcKxzHLrKp+mAEdPEXC7g==")
}

type TestLicense struct {
	TestPublicKey  ed25519.PublicKey
	TestPrivateKey ed25519.PrivateKey

	License *License
	Signed  string
}

func NewTestLicense(flags map[string]interface{}) *TestLicense {
	now := time.Now()
	exp := 1 * time.Hour
	l := &licensing.License{
		LicenseID:       "new-temp-license",
		CustomerID:      "temporary license customer",
		InstallationID:  "*",
		Product:         ProductName,
		IssueTime:       now,
		StartTime:       now,
		ExpirationTime:  now.Add(exp),
		TerminationTime: now.Add(exp),
		Flags:           flags,
	}
	signed, _ := l.SignedString(TestPrivateKey)

	return &TestLicense{
		TestPublicKey:  TestPublicKey,
		TestPrivateKey: TestPrivateKey,
		License:        &License{License: l},
		Signed:         signed,
	}
}

func TestGovernancePolicyFlags() map[string]interface{} {
	return map[string]interface{}{
		"modules": []string{ModuleGovernancePolicy.String()},
	}
}
