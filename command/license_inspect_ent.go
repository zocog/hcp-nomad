// go:build ent

package command

import (
	"fmt"
	"time"

	"github.com/hashicorp/go-licensing/v3"
	nomadLicensing "github.com/hashicorp/nomad-licensing/license"
	"github.com/mitchellh/cli"
)

const (
	EnvNomadLicense     = "NOMAD_LICENSE"
	EnvNomadLicensePath = "NOMAD_LICENSE_PATH"
)

var licenseInspectHelp = fmt.Sprintf(`Inspect and validate a Nomad enterprise license.

Examples:

    $ nomad license inspect /path/to/nomad-license.hclic

    $ export %s=/path/to/nomad-license.hclic
    $ nomad license inspect

    $ export %s=full-license-text-string
    $ nomad license inspect

`, EnvNomadLicensePath, EnvNomadLicense)

func NewLicenseInspectCommand(ui cli.Ui, buildDate time.Time) (cli.Command, error) {
	return licensing.InspectLicenseCommandFactory(ui,
		licensing.WithProductName("nomad"),
		licensing.WithCommandName("nomad license inspect"),
		licensing.WithEnvVars(EnvNomadLicense, EnvNomadLicensePath),
		licensing.WithBuildDate(buildDate),
		licensing.WithExtraValidation(validateLicense),
		licensing.WithExamples(licenseInspectHelp),
	)()
}

func validateLicense(l *licensing.License) error {
	_, err := nomadLicensing.NewLicense(l)
	return err
}
