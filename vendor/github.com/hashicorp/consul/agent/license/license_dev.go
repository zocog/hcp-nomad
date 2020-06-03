// +build consulent
// +build consuldev

package license

const (
	devPublicKey = "OKfAGKf5G6i3eVrNc96f+tlyCQOZrEHjFbZ6AF+Uooc="
)

func init() {
	builtinPublicKeys = append(builtinPublicKeys, devPublicKey)
}
