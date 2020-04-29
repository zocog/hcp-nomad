// +build ent

package nomad

import (
	"encoding/base64"

	nomadLicense "github.com/hashicorp/nomad-licensing/license"
)

// Add nomad-licensing test public key to list of builtin keys for testing
func init() {
	builtinPublicKeys = append(builtinPublicKeys, base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey))
}
