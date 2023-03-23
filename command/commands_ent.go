//go:build ent

package command

import (
	"github.com/hashicorp/nomad/version"
	"github.com/mitchellh/cli"
)

func EntCommands(metaPtr *Meta, agentUi cli.Ui) map[string]cli.CommandFactory {
	return map[string]cli.CommandFactory{
		"license inspect": func() (cli.Command, error) {
			v := version.GetVersion()
			return NewLicenseInspectCommand(agentUi, v.BuildDate)
		},
		"operator snapshot agent": func() (cli.Command, error) {
			return &OperatorSnapshotAgentCommand{
				Meta: *metaPtr,
			}, nil
		},
	}
}
