//go:build ent
// +build ent

package taskrunner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"
	"time"

	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/client/allocdir"
	"github.com/hashicorp/nomad/client/allocrunner/interfaces"
	trtesting "github.com/hashicorp/nomad/client/allocrunner/taskrunner/testing"
	"github.com/hashicorp/nomad/client/config"
	"github.com/hashicorp/nomad/client/taskenv"
	"github.com/hashicorp/nomad/helper/pointer"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	structsc "github.com/hashicorp/nomad/nomad/structs/config"
	"github.com/shoenig/test/must"
)

func Test_templateHook_Prestart_MultiVault(t *testing.T) {
	ci.Parallel(t)

	secretsResp := `
{
  "data": {
    "data": {
      "secret": "secret"
    },
    "metadata": {
      "created_time": "2023-10-18T15:58:29.65137Z",
      "custom_metadata": null,
      "deletion_time": "",
      "destroyed": false,
      "version": 1
    }
  }
}`

	// Start two test servers to simulate Vault cluster responses.
	reqCh := make(chan string)
	defaultVaultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCh <- structs.VaultDefaultCluster
		fmt.Fprintln(w, secretsResp)
	}))
	t.Cleanup(defaultVaultServer.Close)

	otherVaultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCh <- "other"
		fmt.Fprintln(w, secretsResp)
	}))
	t.Cleanup(otherVaultServer.Close)

	// Setup client with several Vault clusters.
	clientConfig := config.DefaultConfig()
	clientConfig.TemplateConfig.DisableSandbox = true
	clientConfig.VaultConfigs = map[string]*structsc.VaultConfig{
		structs.VaultDefaultCluster: {
			Name:    structs.VaultDefaultCluster,
			Enabled: pointer.Of(true),
			Addr:    defaultVaultServer.URL,
		},
		"other": {
			Name:    "other",
			Enabled: pointer.Of(true),
			Addr:    otherVaultServer.URL,
		},
		"disabled": {
			Name:    "disabled",
			Enabled: pointer.Of(false),
		},
	}
	clientConfig.VaultConfig = clientConfig.VaultConfigs[structs.VaultDefaultCluster]

	testCases := []struct {
		name        string
		cluster     string
		expectedErr string
	}{
		{
			name:    "use default cluster",
			cluster: structs.VaultDefaultCluster,
		},
		{
			name:    "use non-default cluster",
			cluster: "other",
		},
		{
			name:        "use disabled cluster",
			cluster:     "disabled",
			expectedErr: "disabled or not configured",
		},
		{
			name:        "use invalid cluster",
			cluster:     "invalid",
			expectedErr: "disabled or not configured",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup alloc and task to connect to Vault cluster.
			alloc := mock.MinAlloc()
			task := alloc.Job.TaskGroups[0].Tasks[0]
			task.Vault = &structs.Vault{
				Cluster: tc.cluster,
			}

			// Setup template hook.
			taskDir := t.TempDir()
			hookConfig := &templateHookConfig{
				logger:       testlog.HCLogger(t),
				lifecycle:    trtesting.NewMockTaskHooks(),
				events:       &trtesting.MockEmitter{},
				clientConfig: clientConfig,
				envBuilder:   taskenv.NewBuilder(mock.Node(), alloc, task, clientConfig.Region),
				templates: []*structs.Template{
					{
						EmbeddedTmpl: `{{with secret "secret/data/test"}}{{.Data.data.secret}}{{end}}`,
						ChangeMode:   structs.TemplateChangeModeNoop,
						DestPath:     path.Join(taskDir, "out.txt"),
					},
				},
			}
			hook := newTemplateHook(hookConfig)

			// Start template hook with a timeout context to ensure it exists.
			req := &interfaces.TaskPrestartRequest{
				Task:    task,
				TaskDir: &allocdir.TaskDir{Dir: taskDir},
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			t.Cleanup(cancel)

			// Start in a goroutine because Prestart() blocks until first
			// render.
			hookErrCh := make(chan error)
			go func() {
				err := hook.Prestart(ctx, req, nil)
				hookErrCh <- err
			}()

			for {
				select {
				// Verify only the Vault cluster being used receives requests.
				case cluster := <-reqCh:
					must.Eq(t, task.Vault.Cluster, cluster)

				// Verify test doesn't timeout.
				case <-ctx.Done():
					must.NoError(t, ctx.Err())
					return

				// Verify expected result of hook.Prestart()
				case err := <-hookErrCh:
					if tc.expectedErr != "" {
						must.ErrorContains(t, err, tc.expectedErr)
					} else {
						must.NoError(t, err)
					}
					return
				}
			}
		})
	}
}
