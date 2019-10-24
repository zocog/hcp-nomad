// Package json contains a Sentinel plugin for parsing and working with
// JSON documents.
package json

import (
	"encoding/json"

	"github.com/hashicorp/sentinel-sdk"
	"github.com/hashicorp/sentinel-sdk/framework"
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
	case "unmarshal":
		return m.unmarshal

	case "marshal":
		return m.marshal
	}

	return nil
}

func (m *root) unmarshal(input string) (interface{}, error) {
	var result interface{}
	err := json.Unmarshal([]byte(input), &result)
	return result, err
}

func (m *root) marshal(input interface{}) (string, error) {
	raw, err := json.Marshal(input)
	return string(raw), err
}
