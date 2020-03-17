// +build ent

package agent

import (
	"testing"

	"github.com/hashicorp/nomad/helper"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/nomad/structs/config"
	"github.com/stretchr/testify/require"
)

func TestSetupEnterpriseAgent(t *testing.T) {
	a := &Agent{
		config: &Config{
			Audit: &config.AuditConfig{
				Enabled: helper.BoolToPtr(true),
			},
		},
	}

	err := a.setupEnterpriseAgent(testlog.HCLogger(t))
	require.NoError(t, err)
}
