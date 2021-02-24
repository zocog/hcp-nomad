// +build ent

package nomad

import (
	"encoding/base64"
	"io"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/go-licensing"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
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
		c.LicenseFilePath = f.Name()
		c.LicenseConfig = &LicenseConfig{
			AdditionalPubKeys: []string{base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey)},
		}
	})

	defer cleanup()

	origLicense := server.EnterpriseState.License()
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

	newConfig := DefaultConfig()
	// path is unchanged; contents were changed
	newConfig.LicenseFilePath = f.Name()

	err = server.Reload(newConfig)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		license := server.EnterpriseState.licenseWatcher.License()
		return license.LicenseID == "some-new-license"
	}, time.Second, 10*time.Millisecond, "Expected license ID to equal 'some-new-license'")

	newLicense := server.EnterpriseState.License()
	require.Equal(t, "some-new-license", newLicense.LicenseID)
}
