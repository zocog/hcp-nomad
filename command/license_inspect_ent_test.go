package command

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	nomadLicensing "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/ci"
	"github.com/mitchellh/cli"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// TestLicenseInspectCommand_RealEnv uses real environment variables, if set,
// as in CI which has a real NOMAD_LICENSE.
func TestLicenseInspectCommand_RealEnv(t *testing.T) {
	ci.Parallel(t)

	license := os.Getenv(EnvNomadLicense)
	path := os.Getenv(EnvNomadLicensePath)
	if license == "" && path == "" {
		t.Skip("NOMAD_LICENSE(_PATH) env var not set")
	}

	ui := new(cli.MockUi)
	cmd, err := NewLicenseInspectCommand(ui, time.Now())
	must.NoError(t, err)

	rc := cmd.Run([]string{})
	stdout := ui.OutputWriter.String()
	stderr := ui.ErrorWriter.String()

	test.Eq(t, 0, rc)
	test.StrContains(t, stdout, "License is valid")
	test.Eq(t, stderr, "")
}

// testLicenseFile provides a license-shaped string saved in a file path.
// It does not use real keys, so will not validate successfully,
// but that logic is tested elsewhere.
func testLicenseFile(t *testing.T) (license, path string) {
	t.Helper()
	lic := nomadLicensing.NewTestLicense(nil)
	path = filepath.Join(t.TempDir(), "nomad.hclic")
	file, err := os.Create(path)
	must.NoError(t, err)
	_, err = file.WriteString(lic.Signed)
	must.NoError(t, err)
	return lic.Signed, path
}

func TestLicenseInspectCommand(t *testing.T) {
	license, path := testLicenseFile(t)
	// We expect this error output because the key signing the license is not real,
	// but its presence indicates that the license was properly passed along to the validator.
	expectErr := "signature invalid for license; tried 1 key"

	// Ensure env vars are not set (e.g. in CI)
	t.Setenv(EnvNomadLicense, "")
	t.Setenv(EnvNomadLicensePath, "")

	for _, tc := range []struct {
		name string
		// command inputs
		args []string
		env  map[string]string
		// expected outputs
		rc  int
		out string
		err string
	}{
		{
			name: "help",
			args: []string{"-h"},
			rc:   0,
			err:  "Usage: nomad license inspect",
		},
		{
			name: "no file",
			rc:   1,
			err:  "No license file specified",
		},
		{
			name: "file arg",
			args: []string{path},
			rc:   1,
			out:  "Source: " + path,
			err:  expectErr,
		},
		{
			name: "env file",
			env:  map[string]string{EnvNomadLicensePath: path},
			rc:   1,
			out:  fmt.Sprintf("path from the %s environment variable", EnvNomadLicensePath),
			err:  expectErr,
		},
		{
			name: "env license",
			env:  map[string]string{EnvNomadLicense: license},
			rc:   1,
			out:  fmt.Sprintf("Source: %s environment variable", EnvNomadLicense),
			err:  expectErr,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ui := new(cli.MockUi)
			cmd, err := NewLicenseInspectCommand(ui, time.Now())
			must.NoError(t, err)

			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			rc := cmd.Run(tc.args)
			stdout := ui.OutputWriter.String()
			stderr := ui.ErrorWriter.String()

			test.Eq(t, tc.rc, rc)
			test.StrContains(t, stdout, tc.out)
			test.StrContains(t, stderr, tc.err)
		})
	}
}
