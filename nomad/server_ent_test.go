//go:build ent
// +build ent

package nomad

import (
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/go-licensing"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/require"
)

func TestServer_Reload_License(t *testing.T) {
	t.Parallel()

	// write license
	issue := time.Now().Add(-time.Hour)
	exp := issue.Add(2 * time.Hour)
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

	server, cleanup := TestServer(t, func(c *Config) {
		c.LicensePath = f.Name()
		c.LicenseEnv = ""
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})

	defer cleanup()

	origLicense := server.EnterpriseState.License()
	require.Equal(t, origLicense.LicenseID, "some-license")

	// write updated license
	l.LicenseID = "some-new-license"
	l.IssueTime = time.Now()
	licenseString, err = l.SignedString(nomadLicense.TestPrivateKey)
	require.NoError(t, err)

	f, err = os.OpenFile(f.Name(), os.O_WRONLY|os.O_TRUNC, 0700)
	require.NoError(t, err)

	_, err = io.WriteString(f, licenseString)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	newConfig := DefaultConfig()
	// path is unchanged; contents were changed
	newConfig.LicensePath = f.Name()

	err = server.Reload(newConfig)
	require.NoError(t, err)

	testutil.WaitForResult(func() (bool, error) {
		license := server.EnterpriseState.licenseWatcher.License()
		if license.LicenseID == "some-new-license" {
			return true, nil
		}
		return false, fmt.Errorf("expected license ID to equal 'some-new-license' got: %s", license.LicenseID)
	}, func(err error) {
		require.Fail(t, err.Error())
	})

}
