// +build ent

package nomad

import (
	"crypto/ed25519"
	"encoding/base64"
)

var (
	TestPublicKey  ed25519.PublicKey
	TestPrivateKey ed25519.PrivateKey
)

func init() {
	TestPublicKey, _ = base64.StdEncoding.DecodeString("K8hQGHP8/KtdLZ0tUJkKllYXCscxy6yqfpgBHTxFwu4=")
	TestPrivateKey, _ = base64.StdEncoding.DecodeString("GipwbPnbVtTZ+/GHuzdXvlPDUjw/HCq3zTygy6atqT0ryFAYc/z8q10tnS1QmQqWVhcKxzHLrKp+mAEdPEXC7g==")

	builtinPublicKeys = append(builtinPublicKeys, base64.StdEncoding.EncodeToString(TestPublicKey))
}
