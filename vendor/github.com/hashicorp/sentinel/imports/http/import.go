// Package http contains a Sentinel plugin for making HTTP requests and fetching
// data for use in Sentinel policies.
package http

import (
	"fmt"
	"net/url"

	"github.com/mitchellh/mapstructure"

	sdk "github.com/hashicorp/sentinel-sdk"
	"github.com/hashicorp/sentinel-sdk/framework"
)

const (
	// The maximum number of retries allowed.
	maxRetries = 10

	// The maximum request timeout, in seconds.
	maxTimeout = 30
)

type config struct {
	// A setting for the maximum body size to accept. Anything larger will
	// trigger a runtime error. This is a global value that will be used in
	// all HTTP requests.
	MaxResponseBytes int64 `mapstructure:"max_response_bytes"`

	// ProxyAddress allows specifying an HTTP server to use as a proxy. This
	// will affect both http/https operations. The setting exists within the
	// plugin because setting it globally in the calling app would be much too
	// heavy- handed.
	ProxyAddress string `mapstructure:"proxy_address"`

	// Cached copy of the parsed URL. Allows only parsing the URL once, and
	// maintaining the cached copy here in the config.
	proxyURL *url.URL
}

// New creates a new Import.
func New() sdk.Import {
	return &framework.Import{
		Root: &root{},
	}
}

type root struct {
	config        *config
	defaultClient *client
}

// framework.Root impl.
func (m *root) Configure(raw map[string]interface{}) error {
	m.config = &config{}
	if err := decode(raw, m.config); err != nil {
		return err
	}

	// Parse the given ProxyAddress, if any.
	if m.config.ProxyAddress != "" {
		u, err := url.Parse(m.config.ProxyAddress)
		if err != nil {
			return err
		}
		m.config.proxyURL = u
	}

	// Set the default MaxResponseBytes, if none given.
	if m.config.MaxResponseBytes == 0 {
		m.config.MaxResponseBytes = 1024 * 1024 * 8
	}

	m.defaultClient = newClient(m.config)

	return nil
}

// framework.Namespace impl.
func (m *root) Get(key string) (interface{}, error) {
	switch key {
	case "client":
		return m.defaultClient, nil
	default:
		return nil, fmt.Errorf("invalid field %q", key)
	}
}

// framework.Call impl.
func (m *root) Func(key string) interface{} {
	switch key {
	case "request":
		return newRequest
	default:
		// Automatically call through to the client namespace. This allows the
		// namespace to fork itself easily while retaining a reference back to
		// the root config for import-level configuration data etc. This
		// facilitates the builder pattern the HTTP import uses nicely.
		return m.defaultClient.Func(key)
	}
}

// framework.New impl.
func (m *root) New(data map[string]interface{}) (framework.Namespace, error) {
	// Get the special _type field value and remove it from the map. This
	// allows mapstructure to cleanly decode while still checking for further
	// unused keys.
	t, ok := data["_type"].(string)
	if !ok {
		return nil, nil
	}
	delete(data, "_type")

	switch t {
	case "client":
		client := m.defaultClient.clone()
		if err := decode(data, client); err != nil {
			return nil, err
		}
		return client, nil
	case "request":
		req := &request{}
		if err := decode(data, req); err != nil {
			return nil, err
		}
		return req, nil
	default:
		return nil, fmt.Errorf("invalid type for namespace")
	}
}

// decode is a strict mapstructure helper.
func decode(input, output interface{}) error {
	decoderConfig := &mapstructure.DecoderConfig{
		ErrorUnused:      true,
		Metadata:         nil,
		Result:           output,
		WeaklyTypedInput: false,
	}

	decoder, err := mapstructure.NewDecoder(decoderConfig)
	if err != nil {
		return err
	}

	return decoder.Decode(input)
}
