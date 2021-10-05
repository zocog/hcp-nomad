//go:build ent
// +build ent

package nomad

import (
	"fmt"
	"testing"

	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/nomad/structs/config"
	vaultconsts "github.com/hashicorp/vault/sdk/helper/consts"
	"github.com/stretchr/testify/require"
)

func TestTokenAuthForTask(t *testing.T) {
	t.Parallel()

	tr := true
	defaultNs := "test-namespace"
	conf := &config.VaultConfig{
		Addr:      "https://vault.service.consul:8200",
		Enabled:   &tr,
		Token:     "testvaulttoken",
		Role:      "test-nomad",
		Namespace: defaultNs,
	}
	logger := testlog.HCLogger(t)

	ent := &VaultEntDelegate{
		l:              logger,
		featureChecker: &testChecker{},
	}

	c, err := NewVaultClient(conf, logger, nil, ent)
	require.NoError(t, err)

	nsClient, err := c.entHandler.clientForTask(c, "new-namespace")

	require.NoError(t, err)
	require.Equal(t, "new-namespace", nsClient.Headers().Get(vaultconsts.NamespaceHeaderName))
}

func TestTokenAuthForTask_Unlicensed_Requested_Different_NS(t *testing.T) {
	t.Parallel()

	tr := true
	defaultNs := "test-namespace"
	conf := &config.VaultConfig{
		Addr:      "https://vault.service.consul:8200",
		Enabled:   &tr,
		Token:     "testvaulttoken",
		Role:      "test-nomad",
		Namespace: defaultNs,
	}
	logger := testlog.HCLogger(t)

	ent := &VaultEntDelegate{
		l:              logger,
		featureChecker: &testChecker{fail: true},
	}

	c, err := NewVaultClient(conf, logger, nil, ent)
	require.NoError(t, err)

	nsClient, err := c.entHandler.clientForTask(c, "new-namespace")
	require.Error(t, err)
	require.Nil(t, nsClient)
}

type testChecker struct {
	fail bool
}

func (t *testChecker) FeatureCheck(feature license.Features, emitLog bool) error {
	if t.fail {
		return fmt.Errorf("Feature %q is unlicensed", feature.String())
	}
	return nil
}
