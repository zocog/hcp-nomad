//go:build ent
// +build ent

package agent

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/audit"
	"github.com/hashicorp/nomad/command/agent/event"
	"github.com/hashicorp/nomad/nomad/structs/config"
	"github.com/pkg/errors"
)

// Ensure audit.Auditor is an Eventer
var _ event.Auditor = &audit.Auditor{}

// EnterpriseAgent allows Agents to interact with enterprise features
type EnterpriseAgent struct {
	featurechecker license.FeatureChecker
}

type NoOpChecker struct{}

func (n *NoOpChecker) FeatureCheck(feature license.Features, emitLog bool) error {
	return nil
}

type AlwaysFailChecker struct{}

func (a *AlwaysFailChecker) FeatureCheck(feature license.Features, emitLog bool) error {
	return fmt.Errorf("Feature %q is unlicensed", feature.String())
}

func (a *Agent) setupEnterpriseAgent(logger hclog.InterceptLogger) error {

	// Determine if we are setting up a server or client
	// and use the respective feature checker
	if a.server != nil {
		a.EnterpriseAgent = &EnterpriseAgent{
			featurechecker: &a.server.EnterpriseState,
		}
	} else if a.client != nil {
		// no-op feature checker until client is implemented
		a.EnterpriseAgent = &EnterpriseAgent{
			featurechecker: a.client.EnterpriseClient,
		}
	} else {
		a.EnterpriseAgent = &EnterpriseAgent{
			featurechecker: &AlwaysFailChecker{},
		}
	}

	// Ensure we have at least empty Auditor config
	if a.config.Audit == nil {
		a.config.Audit = DefaultConfig().Audit
	}

	// Setup auditor
	auditor, err := a.setupAuditor(a.config.Audit, logger)
	if err != nil {
		return errors.Wrap(err, "error configuring auditor")
	}

	// set auditor
	a.auditor = auditor

	return nil
}

func (a *Agent) setupAuditor(cfg *config.AuditConfig, logger hclog.InterceptLogger) (*audit.Auditor, error) {
	var enabled bool
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}

	var filters []audit.FilterConfig
	for _, f := range cfg.Filters {
		filterType := audit.FilterType(f.Type)
		if !filterType.Valid() {
			return nil, fmt.Errorf("Invalid filter type %s", f.Type)
		}

		filter := audit.FilterConfig{
			Type:       filterType,
			Endpoints:  f.Endpoints,
			Stages:     f.Stages,
			Operations: f.Operations,
		}
		filters = append(filters, filter)
	}

	var sinks []audit.SinkConfig
	for _, s := range cfg.Sinks {
		// Split file name from path
		dir, file := filepath.Split(s.Path)

		// If path is provided, but no filename, then default is used.
		if file == "" {
			file = "audit.log"
		}

		// Check that the sink type is valid
		sinkType := audit.SinkType(s.Type)
		if !sinkType.Valid() {
			return nil, fmt.Errorf("Invalid sink type %s", s.Type)
		}

		// Check that the sink format is valid
		sinkFmt := audit.SinkFormat(s.Format)
		if !sinkFmt.Valid() {
			return nil, fmt.Errorf("Invalid sink format %s", s.Format)
		}

		// Set default delivery guarantee to enforced
		if s.DeliveryGuarantee == "" {
			s.DeliveryGuarantee = "enforced"
		}

		delivery := audit.RunMode(s.DeliveryGuarantee)
		if !delivery.Valid() {
			return nil, fmt.Errorf("Invalid delivery guarantee %s", s.DeliveryGuarantee)
		}

		// Set default file mode
		if s.Mode == "" {
			s.Mode = "0600"
		}

		fileModeInt, err := strconv.ParseUint(s.Mode, 8, 32)
		if err != nil {
			return nil, fmt.Errorf("Invalid file mode %q: Must be a valid octal number (%v).", s.Mode, err)
		}

		// fileMode must be a valid file permission
		fileMode := fs.FileMode(fileModeInt)
		if fileMode.Perm() != fileMode {
			return nil, fmt.Errorf("Invalid file mode %q: Must be a valid Unix permission (%s).", s.Mode, fileMode)
		}

		sink := audit.SinkConfig{
			Name:              s.Name,
			Type:              sinkType,
			DeliveryGuarantee: delivery,
			Format:            sinkFmt,
			FileName:          file,
			Path:              dir,
			RotateDuration:    s.RotateDuration,
			RotateBytes:       s.RotateBytes,
			RotateMaxFiles:    s.RotateMaxFiles,
			Mode:              fileMode,
		}
		sinks = append(sinks, sink)
	}

	// Configure default sink if none are configured
	if len(sinks) == 0 {
		defaultSink := audit.SinkConfig{
			Name:     "default-sink",
			Type:     audit.FileSink,
			Format:   audit.JSONFmt,
			FileName: "audit.log",
			Path:     filepath.Join(a.config.DataDir, "audit"),
		}
		sinks = append(sinks, defaultSink)
	}

	auditCfg := &audit.Config{
		Enabled:        enabled,
		Filters:        filters,
		Sinks:          sinks,
		Logger:         logger.ResetNamedIntercept("audit"),
		FeatureChecker: a.EnterpriseAgent.featurechecker,
	}

	auditor, err := audit.NewAuditor(auditCfg)
	if err != nil {
		return nil, err
	}

	return auditor, nil
}

// entReloadEventer enables or disables the eventer and calls reopen.
// Assumes caller has nil checked cfg
func (a *Agent) entReloadEventer(cfg *config.AuditConfig) error {
	enabled := cfg.Enabled != nil && *cfg.Enabled
	a.auditor.SetEnabled(enabled)

	return nil
}
