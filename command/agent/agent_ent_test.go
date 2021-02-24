// +build ent

package agent

import (
	"io"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/go-licensing"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
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

func TestServer_Reload_License(t *testing.T) {
	t.Parallel()

	// write license
	issue := time.Now().Add(-time.Hour)
	exp := issue.Add(time.Hour)
	l := &licensing.License{
		LicenseID:       "some-license",
		CustomerID:      "some-customer",
		InstallationID:  "*",
		Product:         "nomad",
		IssueTime:       issue,
		StartTime:       issue,
		ExpirationTime:  exp,
		TerminationTime: exp,
		Flags:           map[string]interface{}{},
	}

	licenseString, err := l.SignedString(nomadLicense.TestPrivateKey)
	require.NoError(t, err)
	f, err := ioutil.TempFile("", "license.hclic")
	require.NoError(t, err)
	_, err = io.WriteString(f, licenseString)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	defer os.Remove(f.Name())

	agent := NewTestAgent(t, t.Name(), func(c *Config) {
		c.Server = &ServerConfig{
			Enabled:     true,
			LicensePath: f.Name(),
		}
	})

	defer agent.Shutdown()

	require.Eventually(t, func() bool {
		return agent.server != nil
	}, 5*time.Second, 10*time.Millisecond, "Expected agent.server not to be nil")

	origLicense := agent.server.EnterpriseState.License()
	require.Equal(t, origLicense.LicenseID, "some-license")

	// write updated license
	l.LicenseID = "some-new-license"
	licenseString, err = l.SignedString(nomadLicense.TestPrivateKey)
	require.NoError(t, err)

	f, err = os.OpenFile(f.Name(), os.O_WRONLY|os.O_TRUNC, 0700)
	require.NoError(t, err)

	_, err = io.WriteString(f, licenseString)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	newConfig := &Config{
		Server: &ServerConfig{
			Enabled: true,
			// path is unchanged; contents were changed
			LicensePath: f.Name(),
		},
	}

	err = agent.Reload(newConfig)
	require.NoError(t, err)

	newLicense := agent.server.EnterpriseState.License()
	require.Equal(t, newLicense.LicenseID, "some-new-license")
}
