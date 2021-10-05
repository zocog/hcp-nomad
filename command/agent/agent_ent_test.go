//go:build ent
// +build ent

package agent

import (
	"io/ioutil"
	"os"
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
				Filters: []*config.AuditFilter{
					{
						Type:       "HTTPEvent",
						Endpoints:  []string{"/v1/agent/health", "ui"},
						Stages:     []string{"OperationComplete", "OperationReceived"},
						Operations: []string{"GET", "PUT", "POST", "DELETE"},
					},
				},
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

// TestSetupEnterpriseAgent_Disabled ensures a disabled, unconfigured
// Eventer can be configured without error
func TestSetupEnterpriseAgent_Disabled(t *testing.T) {
	a := &Agent{
		config: &Config{
			Audit: &config.AuditConfig{},
		},
	}

	err := a.setupEnterpriseAgent(testlog.HCLogger(t))
	require.NoError(t, err)

	// Ensure eventer is disabled
	require.False(t, a.auditor.Enabled())
}

func TestEntReloadEventer(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	a := &Agent{
		config: &Config{
			Audit: &config.AuditConfig{
				Enabled: helper.BoolToPtr(true),
				Sinks: []*config.AuditSink{
					{
						Name:              "file-sink",
						Path:              tmpDir + "audit.log",
						DeliveryGuarantee: "best-effort",
						Type:              "file",
						Format:            "json",
					},
				},
			},
		},
	}

	// Setup eventer
	err = a.setupEnterpriseAgent(testlog.HCLogger(t))
	require.NoError(t, err)

	// Reload and disable

	cfg := &config.AuditConfig{
		Enabled: helper.BoolToPtr(false),
	}

	err = a.entReloadEventer(cfg)
	require.NoError(t, err)

	require.False(t, a.auditor.Enabled())
}
