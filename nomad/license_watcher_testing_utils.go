// +build ent pro

package nomad

import (
	"encoding/base64"
	"testing"

	nomadLicense "github.com/hashicorp/nomad-licensing/license"
)

// TestValidationHelper must be called before a server is initialized
// It should only be used in tests where a new test license needs to be
// created.
func TestValidationHelper(t *testing.T) {
	builtinPublicKeys = append(builtinPublicKeys, base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey))
}
