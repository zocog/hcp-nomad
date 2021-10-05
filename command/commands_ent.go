//go:build ent
// +build ent

package command

import "github.com/mitchellh/cli"

func EntCommands(metaPtr *Meta, agentUi cli.Ui) map[string]cli.CommandFactory {
	return map[string]cli.CommandFactory{
		"operator snapshot agent": func() (cli.Command, error) {
			return &OperatorSnapshotAgentCommand{
				Meta: *metaPtr,
			}, nil
		},
	}
}
