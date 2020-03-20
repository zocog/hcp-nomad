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
				Sinks: []*config.AuditSink{
					{
						Name:              "file-sink",
						Path:              "/tmp/path/audit.log",
						DeliveryGuarantee: "best-effort",
						Type:              "file",
						Format:            "json",
					},
				},
			},
		},
	}

	err := a.setupEnterpriseAgent(testlog.HCLogger(t))
	require.NoError(t, err)
}
