// +build ent

package audit

import (
	"context"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/hashicorp/eventlogger"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/helper/uuid"
)

// RunMode specifies how errors should be handled when processing an event.
// A RunMode of enforced will return all errors. A RunMode of best effort
// will log the errors and return without errors. This is meant to prevent
// blocking requests from completing when there is an underlying issue with
// the auditing pipeline.
type RunMode string

const (
	// Enforced RunMode ensures an error is returned upon event failure.
	Enforced RunMode = "enforced"
	// BestEffort will log errors to prevent blocking requests from completing.
	BestEffort RunMode = "best-effort"
)

// SinkType specifies the type of sink for the audit log pipeline. Currently
// only file sinks with JSON SinkFormat are supported.
type SinkType string

// FileSink is a SinkType indicating file.
const FileSink SinkType = "file"

// SinkFormat specifies the output format for a sink. Currently only
// JSON is supported.
type SinkFormat string

// JSONFmt is a SinkFormat indicating JSON output.
const JSONFmt SinkFormat = "json"

const (
	AuditPipeline = "audit-pipeline"

	// AuditEvent is an eventlogger EventType that is used to specify auditing.
	// related events.
	AuditEvent eventlogger.EventType = "audit"

	// SchemaV1 is the v1 schema version.
	SchemaV1 = 1
)

// Config determines how an Auditor should be configured
type Config struct {
	Enabled bool
	Filters []FilterConfig
	Sinks   []SinkConfig

	Logger         hclog.InterceptLogger
	FeatureChecker license.FeatureChecker
}

// FilterConfig holds the options and settings to create a Filter.
type FilterConfig struct {
	// Type specifies the type of filter that should be created.
	Type FilterType

	// Endpoints is a collection of endpoints that a filter should apply to.
	Endpoints []string

	// Stages specify which stage of an audit log request a filter should be
	// applied to. OperationReceived, OperationCreated,* (all stages).
	Stages []string

	// Operations specify which operations a filter should apply to. For
	// HTTPFilter types this equates to HTTP verbs.
	Operations []string
}

// SinkConfig holds the configuration options for creating an audit log sink.
type SinkConfig struct {
	// Name is the name of the sink.
	Name string

	// Type is the SinkType of the sink.
	Type SinkType

	// Format is the output format for the sink (json).
	Format SinkFormat

	// DeliveryGuarantee specifies how strict we handle errors.
	DeliveryGuarantee RunMode

	// FileName is the name of the audit log. If file rotation is enabled
	// It will be post-fixed by a timestamp
	FileName string

	// Path is the location where audit log(s) are stored.
	Path string

	// RotateDuration is the length of time between file rotations.
	RotateDuration time.Duration

	// RotateBytes is the amount of bytes that can be written to a file
	// before it is rotated.
	RotateBytes int

	// RotateMaxFiles is the maximum number of old audit log files
	// that can be retained before pruning.
	RotateMaxFiles int

	// Mode is the permissions for the audit log files.
	Mode fs.FileMode
}

// Auditor is the main struct for sending auditing events. If configured
// broker will send events of the event type et through all configured
// filters and sinks
type Auditor struct {
	broker *eventlogger.Broker
	log    hclog.InterceptLogger
	mode   RunMode

	// l protects enabled
	l       sync.RWMutex
	enabled bool

	f license.FeatureChecker
}

// NewAuditor creates an auditor which can be used to send events
// to a file sink and filter based off of specified criteria. Will return
// an error if not properly configured.
func NewAuditor(cfg *Config) (*Auditor, error) {
	if cfg.Logger == nil {
		return nil, errors.New("no logger configured")
	}

	var nodeIDs []eventlogger.NodeID
	broker := eventlogger.NewBroker()

	// Create and register validator node
	validatorID := eventlogger.NodeID(uuid.Generate())
	validator := NewValidator(cfg)
	err := broker.RegisterNode(validatorID, validator)
	if err != nil {
		return nil, errors.Wrap(err, "creating validator node")
	}
	nodeIDs = append(nodeIDs, validatorID)

	// Create and register health check filter
	hcheckID := eventlogger.NodeID(uuid.Generate())
	hcheckFilter := &HealthCheckFilter{}
	err = broker.RegisterNode(hcheckID, hcheckFilter)
	if err != nil {
		return nil, errors.Wrap(err, "creating health check filter node")
	}
	nodeIDs = append(nodeIDs, hcheckID)

	// Configure and generate filters
	filters, err := generateFiltersFromConfig(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate filters")
	}

	// Register filters to broker
	for id, n := range filters {
		err := broker.RegisterNode(id, n)
		if err != nil {
			return nil, errors.Wrap(err, "failed to register filter nodes")
		}
		// Add filter ID to nodeIDs
		nodeIDs = append(nodeIDs, id)
	}

	// Create JSONFormatter node
	jsonfmtID := eventlogger.NodeID(uuid.Generate())
	fmtNode := &eventlogger.JSONFormatter{}
	err = broker.RegisterNode(jsonfmtID, fmtNode)
	if err != nil {
		return nil, errors.Wrap(err, "failed to register json node")
	}
	// Add jsonfmtID to nodeIDs
	nodeIDs = append(nodeIDs, jsonfmtID)

	// Create sink nodes
	sinks, successThreshold, err := generateSinksFromConfig(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate sinks")
	}

	// Register sink nodes
	for id, s := range sinks {
		err := broker.RegisterNode(id, s)
		if err != nil {
			return nil, errors.Wrap(err, "failed to register sink")
		}
		nodeIDs = append(nodeIDs, id)
	}

	// Register pipeline to broker
	err = broker.RegisterPipeline(eventlogger.Pipeline{
		EventType:  AuditEvent,
		PipelineID: AuditPipeline,
		NodeIDs:    nodeIDs,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to register pipeline")
	}

	// Set successThreshold
	// TODO(drew) go-eventlogger SetSuccessThreshold currently does not
	// specify which sink passed and which hasn't so we are unable to
	// support multiple sinks with different delivery guarantees
	err = broker.SetSuccessThreshold(AuditEvent, successThreshold)
	if err != nil {
		return nil, errors.Wrap(err, "failed to set success threshold")
	}

	mode := BestEffort
	if successThreshold > 0 {
		mode = Enforced
	}

	if cfg.FeatureChecker == nil {
		return nil, fmt.Errorf("Developer error, misconfigured server/client feature checker nil")
	}

	return &Auditor{
		enabled: cfg.Enabled,
		broker:  broker,
		log:     cfg.Logger,
		f:       cfg.FeatureChecker,
		mode:    mode,
	}, nil
}

// Enabled returns whether or not an auditor is enabled
func (a *Auditor) Enabled() bool {
	a.l.RLock()
	defer a.l.RUnlock()
	return a.enabled
}

// Event is used to send an audit log event.
func (a *Auditor) Event(ctx context.Context, eventType string, payload interface{}) error {
	// Fast-path if disabled.
	if !a.Enabled() {
		return nil
	}

	// Feature check
	if err := a.f.FeatureCheck(license.FeatureAuditLogging, true); err != nil {
		// Return without error if audit logging is unlicensed
		return nil
	}

	status, err := a.broker.Send(ctx, AuditEvent, payload)
	if err != nil {
		// Only return error if mode is enforced
		if a.mode == Enforced {
			return err
		}
		// If run mode isn't inforced, log that we encountered an error
		a.log.Error("Failed to complete audit log. RunMode not enforced", "err", err.Error())
	}

	if len(status.Warnings) > 0 {
		a.log.Warn("encountered warnings writing events", "warnings:", status.Warnings)
	}

	return nil
}

// Reopen is used during a SIGHUP to reopen nodes, most importantly the underlying
// file sink.
func (a *Auditor) Reopen() error {
	return a.broker.Reopen(context.Background())
}

// SetEnabled sets the auditor to enabled or disabled
func (a *Auditor) SetEnabled(enabled bool) {
	a.l.Lock()
	defer a.l.Unlock()

	a.enabled = enabled
}

// DeliveryEnforced is a way for callers that do not have full control
// over error handling to check if DeliveryGuarantee is Enforced.
// This allows callers to determine if they should swallow and log
// errors instead of returning them, blocking a request
func (a *Auditor) DeliveryEnforced() bool {
	return a.mode == Enforced
}

func generateFiltersFromConfig(cfg *Config) (map[eventlogger.NodeID]eventlogger.Node, error) {
	nodeMap := make(map[eventlogger.NodeID]eventlogger.Node)

	for _, f := range cfg.Filters {
		switch f.Type {
		case HTTPEvent:
			nodeID := eventlogger.NodeID(uuid.Generate())
			node, err := NewHTTPFilter(cfg.Logger, f)
			if err != nil {
				return nil, err
			}
			nodeMap[nodeID] = node
		default:
			return nil, fmt.Errorf("Unsuppoorted filter type %s", f.Type)
		}
	}
	return nodeMap, nil
}

// Valid checks is a given SinkType is valid
func (s SinkType) Valid() bool {
	switch s {
	case FileSink:
		return true
	default:
		return false
	}
}

// Valid checks is a given SinkFormat is valid
func (s SinkFormat) Valid() bool {
	switch s {
	case JSONFmt:
		return true
	default:
		return false
	}
}

// Valid checks is a given RunMode is valid
func (r RunMode) Valid() bool {
	switch r {
	case Enforced, BestEffort:
		return true
	default:
		return false
	}
}

func generateSinksFromConfig(cfg *Config) (map[eventlogger.NodeID]eventlogger.Node, int, error) {
	sinks := make(map[eventlogger.NodeID]eventlogger.Node)
	successThreshold := 0

	if len(cfg.Sinks) > 1 {
		return nil, 0, errors.New("multiple sinks not currently supported")
	}

	for _, s := range cfg.Sinks {
		switch s.Type {
		case FileSink:
			nodeID, node := newFileSink(s)
			sinks[nodeID] = node
			// Increase successThreshold for Guaranteed Delivery
			if s.DeliveryGuarantee == Enforced {
				successThreshold++
			}
		default:
			return nil, successThreshold, fmt.Errorf("Unsuppoorted sink type %s", s.Type)
		}
	}
	return sinks, successThreshold, nil
}

func newFileSink(s SinkConfig) (eventlogger.NodeID, eventlogger.Node) {
	// TODO:drew eventually creation of a sink will need to ensure
	// that there is a corresponding format node for the sink's
	// format, currently JSON is the only option and is statically defined.

	// TODO: DeliveryGuarantee will eventually be defined at the sink level
	sinkID := eventlogger.NodeID(uuid.Generate())
	sink := &eventlogger.FileSink{
		Format:      string(s.Format),
		Path:        s.Path,
		FileName:    s.FileName,
		Mode:        s.Mode,
		MaxBytes:    s.RotateBytes,
		MaxDuration: s.RotateDuration,
		MaxFiles:    s.RotateMaxFiles,
	}
	return sinkID, sink
}
