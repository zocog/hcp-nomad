// Package runtime contains various information about the Sentinel runtime. It
// can be used to get information about the runtime as it's embedded in the
// Simulator or a specific integration.
package runtime

import (
	"github.com/hashicorp/sentinel-sdk"
	"github.com/hashicorp/sentinel-sdk/framework"
	"github.com/hashicorp/sentinel/version"
)

// New creates a new Import.
func New() sdk.Import {
	return &framework.Import{
		Root: &root{},
	}
}

type root struct{}

// framework.Root impl.
func (m *root) Configure(raw map[string]interface{}) error {
	return nil
}

// framework.Namespace impl.
func (m *root) Get(key string) (interface{}, error) {
	switch key {
	case "version":
		return version.VersionString(), nil
	}

	return nil, nil
}
