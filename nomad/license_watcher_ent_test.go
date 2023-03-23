//go:build ent

package nomad

import (
	"io"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	licensing "github.com/hashicorp/go-licensing/v3"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/testutil"
	"github.com/shoenig/test/must"
)

// TestLicenseWatcher_Init_MissingLicenseFile verifies that during startup a
// missing license file fails the LicenseWatcher start
func TestLicenseWatcher_Init_MissingLicenseFile(t *testing.T) {
	ci.Parallel(t)

	cfg := &LicenseConfig{
		Logger: hclog.NewInterceptLogger(nil),
	}

	lw, err := NewLicenseWatcher(cfg)
	must.Nil(t, lw)
	must.Error(t, err)
	must.EqError(t, err, "failed to read license: license is missing. To add a license, configure \"license_path\" in your server configuration file, use the NOMAD_LICENSE environment variable, or use the NOMAD_LICENSE_PATH environment variable. For a trial license of Nomad Enterprise, visit https://nomadproject.io/trial.")
}

// TestLicenseWatcher_Init_InvalidLicenseFileFormat verifies that during startup an
// invalid license file format fails the LicenseWatcher start
func TestLicenseWatcher_Init_InvalidLicenseFileFormat(t *testing.T) {
	ci.Parallel(t)

	cfg := defaultTestLicenseConfig()
	cfg.LicenseEnvBytes = "invalid-license"

	lw, err := NewLicenseWatcher(cfg)
	must.Nil(t, lw)
	must.Error(t, err)
	must.EqError(t, err,
		"failed to initialize nomad license: error decoding version: expected integer")
}

func TestLicenseWatcher_Init_InvalidKeys(t *testing.T) {
	ci.Parallel(t)

	cfg := defaultTestLicenseConfig()
	cfg.AdditionalPubKeys = []string{"invalid pubkey"}
	cfg.LicenseEnvBytes = testLicense(
		"bad-signature-bytes",
		time.Now(),
		time.Now().Add(15*time.Minute),
	)

	lw, err := NewLicenseWatcher(cfg)
	must.Nil(t, lw)
	must.Error(t, err)
	must.EqError(t, err, "error initializing licensing validator: failed to parse key string")
}

// TestLicenseWatcher_Init_InvalidLicenseSignature verifies that during startup an
// invalid license file fails the LicenseWatcher start
func TestLicenseWatcher_Init_InvalidLicenseSignature(t *testing.T) {
	ci.Parallel(t)

	cfg := defaultTestLicenseConfig()
	cfg.AdditionalPubKeys = []string{}

	lw, err := NewLicenseWatcher(cfg)
	must.Nil(t, lw)
	must.Error(t, err)
	must.EqError(t, err,
		"failed to initialize nomad license: signature invalid for license; tried 1 key(s)")
}

// TestLicenseWatcher_Init_ExpiredLicenseFile verifies that during startup an
// expired license file fails the LicenseWatcher start
func TestLicenseWatcher_Init_ExpiredLicenseFile(t *testing.T) {
	ci.Parallel(t)

	cfg := defaultTestLicenseConfig()
	cfg.LicenseEnvBytes = testLicense(
		"expired-license",
		time.Now().Add(-1*time.Hour),
		time.Now().Add(-1*time.Second))

	lw, err := NewLicenseWatcher(cfg)
	must.Nil(t, lw)
	must.Error(t, err)
	must.EqError(t, err,
		"failed to initialize nomad license: 1 error occurred:\n\t* license is no longer valid\n\n")
}

// TestLicenseWatcher_Init_FutureBuildDate verifies that a BuildDate later than
// license expiration fails the LicenseWatcher start
func TestLicenseWatcher_Init_FutureBuildDate(t *testing.T) {
	ci.Parallel(t)

	cfg := defaultTestLicenseConfig()
	cfg.BuildDate = time.Now().Add(time.Hour * 24)

	lw, err := NewLicenseWatcher(cfg)
	must.Nil(t, lw)
	must.Error(t, err)
	must.ErrorIs(t, err, licensing.ErrExpirationBeforeBuildDate)
}

// TestLicenseWatcher_Start_ValidLicenseFile_Ok verifies that during startup a
// valid license file is accepted.
func TestLicenseWatcher_Start_ValidLicenseFile_Ok(t *testing.T) {
	ci.Parallel(t)

	s1, cleanupS1 := TestServer(t, nil)
	defer cleanupS1()
	lw := s1.EnterpriseState.licenseWatcher

	testutil.WaitForResult(func() (bool, error) {
		lic := lw.License()
		return lic != nil &&
			uint64(lic.Features) != uint64(0) &&
			!lw.hasFeature(nomadLicense.FeatureAuditLogging) &&
			!lw.hasFeature(nomadLicense.FeatureMultiregionDeployments), nil
	}, func(err error) {
		t.Fatal("expected valid license")
	})
}

// TestLicenseWatcher_Reload_NoOp verifies that, given a running server with a
// valid license file, reloading the configuration without changes is a no-op.
func TestLicenseWatcher_Reload_NoOp(t *testing.T) {
	ci.Parallel(t)

	initLicense := defaultTestLicense()
	cfg := defaultTestLicenseConfig()

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseEnv = initLicense
		c.LicenseConfig = cfg
	})
	defer cleanupS1()
	lw := s1.EnterpriseState.licenseWatcher

	lic := s1.EnterpriseState.License()
	must.NotNil(t, lic)
	must.Eq(t, "test-license", lic.LicenseID)

	must.NoError(t, lw.Reload(cfg))

	lic = s1.EnterpriseState.License()
	must.NotNil(t, lic)
	must.Eq(t, "test-license", lic.LicenseID)

	// reload with an empty config as well
	must.NoError(t, lw.Reload(&LicenseConfig{}))

	lic = s1.EnterpriseState.License()
	must.NotNil(t, lic)
	must.Eq(t, "test-license", lic.LicenseID)
}

// TestLicenseWatcher_Reload_NewValid verifies that, given a running server
// with a valid license file, if the license file is replaced with a valid
// license and the configuration is reloaded, the LicenseWatcher state will be
// updated to the new license.
func TestLicenseWatcher_Reload_NewValid(t *testing.T) {
	ci.Parallel(t)

	initLicense := defaultTestLicense()

	cfg := defaultTestLicenseConfig()

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseEnv = initLicense
		c.LicenseConfig = cfg
	})
	defer cleanupS1()
	lw := s1.EnterpriseState.licenseWatcher

	lic := s1.EnterpriseState.License()
	must.NotNil(t, lic)
	must.Eq(t, "test-license", lic.LicenseID)

	file := testLicense("reload-id", time.Now(), time.Now().Add(1*time.Hour))
	f, err := ioutil.TempFile("", "licensewatcher")
	must.NoError(t, err)
	_, err = io.WriteString(f, file)
	must.NoError(t, err)
	must.NoError(t, f.Close())

	defer os.Remove(f.Name())

	cfg = &LicenseConfig{
		LicensePath:       f.Name(),
		AdditionalPubKeys: encodedTestLicensePubKeys(),
		Logger:            hclog.NewInterceptLogger(nil),
	}

	must.NoError(t, lw.Reload(cfg))

	lic = s1.EnterpriseState.License()
	must.NotNil(t, lic)
	must.Eq(t, "reload-id", lic.LicenseID)
}

// TestLicenseWatcher_Reload_NewExpired verifies that, given a running server
// with a valid license file, if the license file is replaced with an expired
// license and the configuration is reloaded, the LicenseWatcher state will be
// not be updated to the new license.
func TestLicenseWatcher_Reload_NewExpired(t *testing.T) {
	ci.Parallel(t)

	initLicense := testLicense("test",
		time.Now().Add(-5*time.Minute), time.Now().Add(15*time.Minute))
	cfg := defaultTestLicenseConfig()

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseEnv = initLicense
		c.LicenseConfig = cfg
	})
	defer cleanupS1()
	lw := s1.EnterpriseState.licenseWatcher

	lic := s1.EnterpriseState.License()
	must.NotNil(t, lic)
	must.Eq(t, "test", lic.LicenseID)

	expiredLicense := testLicense("expired-license",
		time.Now().Add(-2*time.Minute), time.Now().Add(-1*time.Minute))
	cfg.LicenseEnvBytes = expiredLicense

	err := lw.Reload(cfg)
	must.EqError(t, err,
		"error validating license: 1 error occurred:\n\t* license is no longer valid\n\n")

	lic = s1.EnterpriseState.License()
	must.NotNil(t, lic)
	must.Eq(t, "test", lic.LicenseID)
}

// TestLicenseWatcher_Reload_NewInvalidFormat verifies that, given a running
// server with a valid license file, if the license file is replaced with a
// license with an invalid format and the configuration is reloaded, the
// LicenseWatcher state will be not be updated to the new license.
func TestLicenseWatcher_Reload_NewInvalidFormat(t *testing.T) {
	ci.Parallel(t)

	initLicense := testLicense("test",
		time.Now().Add(-5*time.Minute), time.Now().Add(15*time.Minute))
	cfg := defaultTestLicenseConfig()

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseEnv = initLicense
		c.LicenseConfig = cfg
	})
	defer cleanupS1()
	lw := s1.EnterpriseState.licenseWatcher

	lic := s1.EnterpriseState.License()
	must.NotNil(t, lic)
	must.Eq(t, "test", lic.LicenseID)

	cfg.LicenseEnvBytes = "invalid-license"

	err := lw.Reload(cfg)
	must.EqError(t, err,
		"error validating license: error decoding version: expected integer")

	lic = s1.EnterpriseState.License()
	must.NotNil(t, lic)
	must.Eq(t, "test", lic.LicenseID)
}

// TestLicenseWatcher_Expired_NoFeatures verifies that, given a running server
// with a valid license file, if the license file expires, we gracefully
// degrade features.
func TestLicenseWatcher_Expired_NoFeatures(t *testing.T) {
	ci.Parallel(t)

	issue := time.Now().Add(-5 * time.Minute)
	exp := time.Now().Add(2 * time.Second)
	lic := &licensing.License{
		LicenseID:       "test",
		CustomerID:      "test customer id",
		InstallationID:  "*",
		Product:         "nomad",
		IssueTime:       issue,
		StartTime:       issue,
		ExpirationTime:  exp,
		TerminationTime: exp,
		Flags: map[string]interface{}{
			"modules": []interface{}{
				nomadLicense.ModuleGovernancePolicy.String(),
			},
		},
	}
	signed, _ := lic.SignedString(nomadLicense.TestPrivateKey)

	cfg := defaultTestLicenseConfig()

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.LicenseEnv = signed
		c.LicenseConfig = cfg
	})
	defer cleanupS1()
	lw := s1.EnterpriseState.licenseWatcher

	testutil.WaitForResult(func() (bool, error) {
		return !lw.hasFeature(nomadLicense.FeatureAuditLogging), nil
	}, func(err error) {
		t.Fatal("expected license to expire and have no features")
	})
}
