// +build ent

package audit

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/go-eventlogger"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/helper/uuid"
)

type RunMode string
type SinkType string
type SinkFormat string

const (
	AuditPipeline         = "audit-pipeline"
	Enforced      RunMode = "enforced"
	BestEffort    RunMode = "best-effort"

	HTTPEvent  FilterType            = "HTTPEvent"
	FileSink   SinkType              = "file"
	JSONFmt    SinkFormat            = "json"
	AuditEvent eventlogger.EventType = "audit"
)

type Auditor struct {
	broker *eventlogger.Broker
	et     eventlogger.EventType
	log    hclog.InterceptLogger
	mode   RunMode
}

type Config struct {
	Enabled bool
	Filters []Filter
	Sinks   []Sink

	Logger hclog.InterceptLogger
}

type Filter struct {
	Type       FilterType
	Endpoints  []string
	Stages     []string
	Operations []string
}

type Sink struct {
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

	// Create sink nodes
	sinks, err := generateSinksFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	// Register sink nodes
	for id, n := range sinks {
		err := broker.RegisterNode(id, n)
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

	return &Auditor{
		broker: broker,
		et:     AuditEvent,
	}, nil
}

func (a *Auditor) Event(ctx context.Context, eventType string, payload interface{}) error {
	status, err := a.broker.Send(ctx, a.et, payload)
	if err != nil {
		return err
	}

	if len(status.Warnings) > 0 {
		a.log.Warn("Auditor: encountered warnings writing events", "warnings:", status.Warnings)
	}

	return nil
}

// Event is used to send Events through the auditing pipeline
// Will return an error depending on configured delivery guarantees.
// func (a *Auditor) Event(ctx context.Context, event *Event) error {
// 	status, err := a.broker.Send(ctx, a.et, event)
// 	if err != nil {
// 		return err
// 	}

// 	if len(status.Warnings) > 0 {
// 		a.log.Warn("Auditor: encountered warnings writing events", "warnings:", status.Warnings)
// 	}

// 	return nil
// }

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

func generateSinksFromConfig(cfg *Config) (map[eventlogger.NodeID]eventlogger.Node, error) {
	sinks := make(map[eventlogger.NodeID]eventlogger.Node)

	for _, s := range cfg.Sinks {
		switch s.Type {
		case FileSink:
			nodeID, node, err := newFileSink(s)
			if err != nil {
				return nil, err
			}
			sinks[nodeID] = node

		default:
			return nil, fmt.Errorf("Unsuppoorted sink type %s", s.Type)
		}
	}
	return sinks, nil
}

func newFileSink(s Sink) (eventlogger.NodeID, eventlogger.Node, error) {
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
	return sinkID, sink, nil
}

func (s SinkType) Valid() bool {
	switch s {
	case FileSink:
		return true
	default:
		return false
	}
}

func (s SinkFormat) Valid() bool {
	switch s {
	case JSONFmt:
		return true
	default:
		return false
	}
}

func (r RunMode) Valid() bool {
	switch r {
	case Enforced, BestEffort:
		return true
	default:
		return false
	}
}
