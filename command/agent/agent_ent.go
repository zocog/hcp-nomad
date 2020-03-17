// +build ent

package agent

import (
	"fmt"
	"path/filepath"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/audit"
	"github.com/hashicorp/nomad/command/agent/event"
)

// Ensure audit.Auditor is an Eventer
var _ event.Eventer = &audit.Auditor{}

func (a *Agent) setupEnterpriseAgent(logger hclog.InterceptLogger) error {
	if a.config.Audit.Enabled == nil || *a.config.Audit.Enabled != true {
		// configure disabled auditor
		return nil
	}

	var enabled bool
	if a.config.Audit.Enabled == nil {
		enabled = false
	} else {
		enabled = *a.config.Audit.Enabled
	}

	var filters []audit.Filter
	for _, f := range a.config.Audit.Filters {
		filterType := audit.FilterType(f.Type)
		if !filterType.Valid() {
			return fmt.Errorf("Invalid filter type %s", f.Type)
		}

		filter := audit.Filter{
			Type:       filterType,
			Endpoints:  f.Endpoints,
			Stages:     f.Stages,
			Operations: f.Operations,
		}
		filters = append(filters, filter)
	}

	var sinks []audit.Sink
	for _, s := range a.config.Audit.Sinks {
		// Split file name from path
		dir, file := filepath.Split(s.Path)

		// If path is provided, but no filename, then default is used.
		if file == "" {
			file = "audit.log"
		}

		// Check that the sink type is valid
		sinkType := audit.SinkType(s.Type)
		if !sinkType.Valid() {
			return fmt.Errorf("Invalid sink type %s", s.Type)
		}

		// Check that the sink format is valid
		sinkFmt := audit.SinkFormat(s.Format)
		if !sinkFmt.Valid() {
			return fmt.Errorf("Invalid sink format %s", s.Format)
		}

		delivery := audit.RunMode(s.DeliveryGuarantee)
		if !delivery.Valid() {
			return fmt.Errorf("Invalid delivery guarantee %s", s.DeliveryGuarantee)
		}

		sink := audit.Sink{
			Name:              s.Name,
			Type:              sinkType,
			DeliveryGuarantee: delivery,
			Format:            sinkFmt,
			FileName:          file,
			Path:              dir,
			RotateDuration:    s.RotateDuration,
			RotateBytes:       s.RotateBytes,
			RotateMaxFiles:    s.RotateMaxFiles,
		}
		sinks = append(sinks, sink)
	}

	cfg := &audit.Config{
		Enabled: enabled,
		Filters: filters,
		Sinks:   sinks,
		Logger:  logger.ResetNamedIntercept("audit"),
	}

	auditor, err := audit.NewAuditor(cfg)
	if err != nil {
		return err
	}

	// set eventer
	a.eventer = auditor

	return nil
}
