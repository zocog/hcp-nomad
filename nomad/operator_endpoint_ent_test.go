// +build ent

package nomad

import (
	"encoding/base64"
	"testing"

	msgpackrpc "github.com/hashicorp/net-rpc-msgpackrpc"
	"github.com/hashicorp/nomad-licensing/license"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/require"
)

func TestOperator_SchedulerSetConfiguration_UnLicensed(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
		c.Build = "0.9.0+unittest"
	})
	defer cleanupS1()
	codec := rpcClient(t, s1)
	testutil.WaitForLeader(t, s1.RPC)

	oldLicense, err := s1.EnterpriseState.licenseWatcher.GetLicense()
	require.NoError(err)

	// Apply new license for platform module (no preemption)
	l := license.NewTestLicense(license.TestPlatformFlags())
	_, err = s1.EnterpriseState.licenseWatcher.SetLicense(l.Signed)
	require.NoError(err)

	// Wait for new license to apply
	testutil.WaitForResult(func() (bool, error) {
		newL, err := s1.EnterpriseState.licenseWatcher.GetLicense()
		require.NoError(err)
		return oldLicense.LicenseID != newL.LicenseID, nil
	}, func(err error) {
		require.FailNow("expected new license to be applied")
	})

	// Enable service scheduler preemption without proper license
	arg := structs.SchedulerSetConfigRequest{
		Config: structs.SchedulerConfiguration{
			PreemptionConfig: structs.PreemptionConfig{
				ServiceSchedulerEnabled: true,
			},
		},
	}
	arg.Region = s1.config.Region

	var setResponse structs.SchedulerSetConfigurationResponse
	err = msgpackrpc.CallWithCodec(codec, "Operator.SchedulerSetConfiguration", &arg, &setResponse)
	require.Error(err)
	require.Contains(err.Error(), "unlicensed")
}
