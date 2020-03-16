// +build ent

package agent

import (
	"github.com/hashicorp/nomad/audit"
	"github.com/hashicorp/nomad/command/agent/event"
)

var _ event.Eventer = &audit.Auditor{}

func (a *Agent) setupEnterpriseAgent() error {
	if a.config.Audit.Enabled == nil || *a.config.Audit.Enabled != true {

		return nil
	}

	cfg := &audit.Config{
		// Enabled:
	}
	auditor, err := audit.NewAuditor(cfg)
	if err != nil {
		return err
	}

	a.eventer = auditor

	return nil
}
