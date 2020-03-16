// +build ent

package agent

import (
	"testing"

	"github.com/hashicorp/nomad/helper"
	"github.com/hashicorp/nomad/nomad/structs/config"
	"github.com/stretchr/testify/require"
)

func TestsetupEnterpriseAgent(t *testing.T) {
	a := &Agent{
		config: &Config{
			Audit: &config.AuditConfig{
				Enabled: helper.BoolToPtr(true),
			},
		},
	}

	err := a.setupEnterpriseAgent()
	require.NoError(t, err)
}
