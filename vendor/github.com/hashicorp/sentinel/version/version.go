// This file is auto-generated - DO NOT EDIT
package version

import (
	"fmt"
)

// The main version number that is being run at the moment.
const Version = "0.12.1"

// A pre-release marker for the version. If this is "" (empty string)
// then it means that it is a final release. Otherwise, this is a pre-release
// such as "dev" (in development), "beta", "rc1", etc.
const VersionPrerelease = "dev"

// VersionString returns the complete version string.
func VersionString() string {
	if VersionPrerelease != "" {
		return fmt.Sprintf("%s-%s", Version, VersionPrerelease)
	}

	return Version
}
