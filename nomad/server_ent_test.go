//go:build ent

package nomad

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-licensing/v3"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/command/agent/consul"
	"github.com/hashicorp/nomad/testutil"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/stretchr/testify/require"
)

// TestServer_Bad_License verifies that an invalid license prevents the server from writing state,
// mainly so an upgrade that fails due to license expiration can be rolled back.
func TestServer_Bad_License(t *testing.T) {
	ci.Parallel(t)

	config := TestConfigForServer(t)
	config.DevMode = false // raft should actually happen if our expected error doesn't occur
	config.LicenseConfig.BuildDate = time.Now().Add(time.Hour * 24 * 365)

	s, err := NewServer(config, &consul.MockCatalog{}, &consul.MockConfigsAPI{}, &consul.MockACLsAPI{})
	t.Cleanup(func() { // in case somehow it manages to start
		if s == nil {
			return
		}
		if e := s.Shutdown(); e != nil {
			t.Error(e)
		}
	})

	// check that raft didn't happen. this is pretty fragile, but it's something.
	// its logger gets set pretty early in Server.setupRaft()
	test.Nil(t, config.RaftConfig.Logger, test.Sprint("raft logger should be nil"))
	// perhaps less fragile, also double-check whether raft data dir was created.
	// usage of DirNotExistsFS is a bit strange, but the leading slash
	raftDir := strings.TrimLeft(filepath.Join(config.DataDir, raftState), "/")
	test.DirNotExistsFS(t, os.DirFS("/"), raftDir, test.Sprintf("raft dir in %s", config.DataDir))

	// assert that this is all due to our desired error
	test.Nil(t, s, test.Sprint("*Server should be nil"))
	must.Error(t, err)
	must.ErrorContains(t, err, "invalid license config")
}

func TestServer_Reload_License(t *testing.T) {
	ci.Parallel(t)

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
	must.NoError(t, err)
	f, err := ioutil.TempFile("", "license.hclic")
	must.NoError(t, err)
	_, err = io.WriteString(f, licenseString)
	must.NoError(t, err)
	must.NoError(t, f.Close())

	defer os.Remove(f.Name())

	server, cleanup := TestServer(t, func(c *Config) {
		// use the on-disk license instead of the default test one in env
		c.LicenseConfig.LicenseEnvBytes = ""
		c.LicenseConfig.LicensePath = f.Name()
	})

	defer cleanup()

	origLicense := server.EnterpriseState.License()
	must.Eq(t, origLicense.LicenseID, "some-license")

	// write updated license
	l.LicenseID = "some-new-license"
	l.IssueTime = time.Now()
	licenseString, err = l.SignedString(nomadLicense.TestPrivateKey)
	must.NoError(t, err)

	f, err = os.OpenFile(f.Name(), os.O_WRONLY|os.O_TRUNC, 0700)
	must.NoError(t, err)

	_, err = io.WriteString(f, licenseString)
	must.NoError(t, err)
	must.NoError(t, f.Close())

	newConfig := DefaultConfig()
	// path is unchanged; contents were changed
	newConfig.LicenseConfig.LicensePath = f.Name()

	err = server.Reload(newConfig)
	must.NoError(t, err)

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
