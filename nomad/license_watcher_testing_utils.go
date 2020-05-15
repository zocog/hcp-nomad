// +build ent

package nomad

import (
	"encoding/base64"
	"testing"

	nomadLicense "github.com/hashicorp/nomad-licensing/license"
)

// TestLicenseValidationHelper must be called before a server is initialized
// It should only be used in tests where a new test license needs to be
// created.
func TestLicenseValidationHelper(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)
	for _, k := range builtinPublicKeys {
		if k == key {
			return
		}
	}

	builtinPublicKeys = append(builtinPublicKeys, key)
}
