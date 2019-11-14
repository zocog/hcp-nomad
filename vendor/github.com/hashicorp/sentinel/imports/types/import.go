// Package types contains a Sentinel plugin for parsing the type of an object.
package types

import (
	"github.com/hashicorp/sentinel-sdk"
	"github.com/hashicorp/sentinel-sdk/framework"
	"github.com/hashicorp/sentinel/runtime/encoding"
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
	return nil, nil
}

// framework.Call impl.
func (m *root) Func(key string) interface{} {
	switch key {
	case "type_of":
		return m.type_of
	}

	return nil
}

func (m *root) type_of(input interface{}) (string, error) {
	obj, err := encoding.GoToObject(input)
	if err != nil {
		return "", err
	}

	return obj.Type().String(), nil
}
