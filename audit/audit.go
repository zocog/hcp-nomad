// +build ent

package audit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/go-eventlogger"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/helper/uuid"
)

// RunMode specifies how errors should be handled when processing an event.
// A RunMode of enforced will return all errors. A RunMode of best effort
// will log the errors and return without errors. This is meant to prevent
// blocking requests from completing when there is an underlying issue with
// the auditing pipeline.
type RunMode string

// SinkType specifies the type of sink for the audit log pipeline. Currently
// only file sinks with JSON SinkFormat are supported.
type SinkType string

// SinkFormat specifies the output format for a sink. Currently only
// JSON is supported.
type SinkFormat string

const (
	AuditPipeline = "audit-pipeline"

	// Enforced RunMode ensures an error is returned upon event failure.
	Enforced RunMode = "enforced"

	// BestEffort will log errors to prevent blocking requests from completing.
	BestEffort RunMode = "best-effort"

	// HTTPEvent is a FilterType to specify a filter should apply to HTTP Events.
	HTTPEvent FilterType = "HTTPEvent"

	// FileSink is a SinkType indicating file.
	FileSink SinkType = "file"

	// JSONFmt is a SinkFormat indicating JSON output.
	JSONFmt SinkFormat = "json"

	// AuditEvent is an eventlogger EventType that is used to specify auditing.
	// related events.
	AuditEvent eventlogger.EventType = "audit"

	// SchemaV1 is the v1 schema version.
	SchemaV1 = 1
)

// Auditor is the main struct for sending auditing events. It's configured
// broker will send events of the event type et
type Auditor struct {
	broker *eventlogger.Broker
	et     eventlogger.EventType
	log    hclog.InterceptLogger
	mode   RunMode

	// l protects enabled
	l       sync.RWMutex
	enabled bool
}

// Config determines how an Auditor should be configured
type Config struct {
	Enabled bool
	Filters []FilterConfig
	Sinks   []SinkConfig

	Logger hclog.InterceptLogger
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
	// The name of the sink
	Name              string
	Type              SinkType
	Format            SinkFormat
	DeliveryGuarantee RunMode
	// Mode
	Mode           os.FileMode
	FileName       string
	Path           string
	RotateDuration time.Duration
	RotateBytes    int
	RotateMaxFiles int
}

// NewAuditor creates an auditor which can be used to send events
// to a file sink and filter based off of specified criteria. Will return
// an error if not properly configured.
func NewAuditor(cfg *Config) (*Auditor, error) {
	var nodeIDs []eventlogger.NodeID
	broker := eventlogger.NewBroker()

	// Configure and generate filters
	filters, err := generateFiltersFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	// Register filters to broker
	for id, n := range filters {
		err := broker.RegisterNode(id, n)
		if err != nil {
			return nil, err
		}
		// Add filter ID to nodeIDs
		nodeIDs = append(nodeIDs, id)
	}

	// Create JSONFormatter node
	jsonfmtID := eventlogger.NodeID(uuid.Generate())
	fmtNode := &eventlogger.JSONFormatter{}
	err = broker.RegisterNode(jsonfmtID, fmtNode)
	if err != nil {
		return nil, err
	}
	// Add jsonfmtID to nodeIDs
	nodeIDs = append(nodeIDs, jsonfmtID)

	if len(cfg.Sinks) > 1 {
		return nil, errors.New("Multiple sinks not currently supported")
	}

	// Create sink nodes
	sinks, successThreshold, err := generateSinksFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	// Register sink nodes
	for id, s := range sinks {
		err := broker.RegisterNode(id, s)
		if err != nil {
			return nil, err
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
		return nil, err
	}

	// set successThreshold
	// TODO(drew) go-eventlogger SetSuccessThreshold currently can't
	// specify which sink passed and which hasn't so we are unable to
	// support multiple sinks with different delivery guarantees
	err = broker.SetSuccessThreshold(AuditEvent, successThreshold)
	if err != nil {
		return nil, err
	}

	mode := BestEffort
	if successThreshold > 0 {
		mode = Enforced
	}

	return &Auditor{
		enabled: cfg.Enabled,
		broker:  broker,
		et:      AuditEvent,
		log:     cfg.Logger,
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
	status, err := a.broker.Send(ctx, a.et, payload)
	if err != nil {
		// Only return error if mode is enforced
		if a.mode == Enforced {
			return err
		}
		// If run mode isn't inforced, log that we encountered an error
		a.log.Error("Failed to complete audit log. RunMode not enforced", "err", err.Error())
	}

	if len(status.Warnings) > 0 {
		a.log.Warn("Auditor: encountered warnings writing events", "warnings:", status.Warnings)
	}

	return nil
}

// Reopen is used during a SIGHUP to reopen nodes, most importantly the underlying
// file sink.
func (a *Auditor) Reopen() error {
	return a.broker.Reopen(context.Background())
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

	for _, s := range cfg.Sinks {
		switch s.Type {
		case FileSink:
			nodeID, node := newFileSink(s)
			sinks[nodeID] = node
			// Increase successThreshold for Guaranteed Delivery
			if s.DeliveryGuarantee == Enforced {
				successThreshold = 1
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
