//go:build ent
// +build ent

package template

import (
	"testing"

	ctconfig "github.com/hashicorp/consul-template/config"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/client/config"
	"github.com/hashicorp/nomad/client/taskenv"
	"github.com/hashicorp/nomad/helper/pointer"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	sconfig "github.com/hashicorp/nomad/nomad/structs/config"
	"github.com/shoenig/test/must"
)

func TestTaskTemplateManager_Config_MultiVault(t *testing.T) {
	ci.Parallel(t)

	vaultConfigs := map[string]*sconfig.VaultConfig{
		structs.VaultDefaultCluster: {
			Enabled: pointer.Of(true),
			Addr:    "http://localhost:4200/",
		},
		"other": {
			Enabled:       pointer.Of(true),
			Addr:          "https://localhost:4300/",
			Namespace:     "other",
			TLSSkipVerify: pointer.Of(true),
		},
		"disabled": {
			Enabled: pointer.Of(false),
			Addr:    "http://localhost:4400/",
		},
	}

	testCases := []struct {
		name           string
		vaultCluster   string
		expectedConfig *ctconfig.VaultConfig
	}{
		{
			name:         "default cluster",
			vaultCluster: structs.VaultDefaultCluster,
			expectedConfig: &ctconfig.VaultConfig{
				Address: pointer.Of("http://localhost:4200/"),
				SSL: &ctconfig.SSLConfig{
					Enabled: pointer.Of(false),
					Verify:  pointer.Of(false),
				},
			},
		},
		{
			name:         "other cluster",
			vaultCluster: "other",
			expectedConfig: &ctconfig.VaultConfig{
				Address:   pointer.Of("https://localhost:4300/"),
				Namespace: pointer.Of("other"),
				SSL: &ctconfig.SSLConfig{
					Enabled: pointer.Of(true),
					Verify:  pointer.Of(false),
				},
			},
		},
		{
			name:         "disabled cluster",
			vaultCluster: "disabled",
			expectedConfig: &ctconfig.VaultConfig{
				Address: pointer.Of(""),
			},
		},
		{
			name:         "non existing cluster",
			vaultCluster: "non-existing",
			expectedConfig: &ctconfig.VaultConfig{
				Address: pointer.Of(""),
			},
		},
		{
			name:         "no vault",
			vaultCluster: "",
			expectedConfig: &ctconfig.VaultConfig{
				Address: pointer.Of(""),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := config.DefaultConfig()
			c.Node = mock.Node()
			c.VaultConfigs = vaultConfigs

			// Use consul-template default so we don't need to update every
			// test case.
			c.TemplateConfig.VaultRetry.Attempts = pointer.Of(ctconfig.DefaultRetryAttempts)

			alloc := mock.Alloc()
			config := &TaskTemplateManagerConfig{
				ClientConfig: c,
				VaultConfig:  vaultConfigs[tc.vaultCluster],
				EnvBuilder:   taskenv.NewBuilder(c.Node, alloc, alloc.Job.TaskGroups[0].Tasks[0], c.Region),
			}

			ctmplMapping, err := parseTemplateConfigs(config)
			must.NoError(t, err)

			ctconf, err := newRunnerConfig(config, ctmplMapping)
			must.NoError(t, err)

			// This value should always be set.
			tc.expectedConfig.RenewToken = pointer.Of(false)

			tc.expectedConfig.Finalize()
			must.Eq(t, tc.expectedConfig, ctconf.Vault)
		})
	}
}
