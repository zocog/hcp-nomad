//go:build !ent
// +build !ent

package agent

// DefaultEntConfig is an empty config in open source
func DefaultEntConfig() *Config {
	return &Config{}
}

func (c *Config) entParseConfig() error {
	return nil
}
