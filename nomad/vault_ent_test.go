//go:build ent
// +build ent

package nomad

import (
	"fmt"
	"testing"

	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/nomad/structs/config"
	"github.com/shoenig/test/must"
)

func TestTokenAuthForTask(t *testing.T) {
	ci.Parallel(t)

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
	must.NoError(t, err)

	nsClient, err := c.entHandler.clientForTask(c, "new-namespace")

	must.NoError(t, err)
	must.Eq(t, "new-namespace", nsClient.Headers().Get(vaultNamespaceHeaderName))
}

func TestTokenAuthForTask_Unlicensed_Requested_Different_NS(t *testing.T) {
	ci.Parallel(t)

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
	must.NoError(t, err)

	nsClient, err := c.entHandler.clientForTask(c, "new-namespace")
	must.Error(t, err)
	must.Nil(t, nsClient)
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
