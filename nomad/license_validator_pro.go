// +build pro

package nomad

import "github.com/hashicorp/go-licensing"

func licenseValidator(lic *licensing.License, licenseStr string) error { return nil }

func TemporaryFlags() map[string]interface{} { return nil }
