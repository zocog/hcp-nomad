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
type FilterType string

const (
	AuditPipeline         = "audit-pipeline"
	Enforced      RunMode = "enforced"
	Disabled      RunMode = "false"
	BestEffort    RunMode = "best-effort"

	HTTPEvent  FilterType            = "HTTPEvent"
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
	RunMode RunMode

	// FileName is the name that the audit log should follow.
	// If rotation is enabled the pattern will be name-timestamp.log
	FileName string

	// Mode
	Mode os.FileMode

	// Path where audit log files should be stored.
	Path string

	MaxBytes    int
	MaxDuration time.Duration
	MaxFiles    int
	Filters     []Filter

	Logger hclog.InterceptLogger
}

type Filter struct {
	Type       FilterType
	Endpoints  []string
	Stages     []string
	Operations []string
}

// NewAuditor creates an auditor which can be used to send events
// to a file sink and filter based off of specified criteria. Will return
// an error if not properly configured.
func NewAuditor(cfg *Config) (*Auditor, error) {
	broker := eventlogger.NewBroker()

	// Configure and generate filters
	filters, err := generateFiltersFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	// Register filters to broker
	var filterIDs []eventlogger.NodeID
	for id, n := range filters {
		err := broker.RegisterNode(id, n)
		if err != nil {
			return nil, err
		}
		filterIDs = append(filterIDs, id)
	}

	// Create JSONFormatter node
	jsonfmtID := eventlogger.NodeID(uuid.Generate())
	fmtNode := &eventlogger.JSONFormatter{}
	err = broker.RegisterNode(jsonfmtID, fmtNode)
	if err != nil {
		return nil, err
	}

	// Configure file sink
	sinkID := eventlogger.NodeID(uuid.Generate())
	sink := &eventlogger.FileSink{
		Path:        cfg.Path,
		FileName:    cfg.FileName,
		Mode:        cfg.Mode,
		MaxBytes:    cfg.MaxBytes,
		MaxDuration: cfg.MaxDuration,
		MaxFiles:    cfg.MaxFiles,
	}
	err = broker.RegisterNode(sinkID, sink)
	if err != nil {
		return nil, err
	}

	// Register pipeline to broker
	var NodeIDs []eventlogger.NodeID
	NodeIDs = append(NodeIDs, filterIDs...)
	NodeIDs = append(NodeIDs, jsonfmtID, sinkID)
	err = broker.RegisterPipeline(eventlogger.Pipeline{
		EventType:  AuditEvent,
		PipelineID: AuditPipeline,
		NodeIDs:    NodeIDs,
	})
	if err != nil {
		return nil, err
	}

	return &Auditor{
		broker: broker,
		et:     AuditEvent,
	}, nil
}

// Event is used to send Events through the auditing pipeline
// Will return an error depending on configured delivery guarantees.
func (a *Auditor) Event(ctx context.Context, event *Event) error {
	status, err := a.broker.Send(ctx, a.et, event)
	if err != nil {
		return err
	}

	if len(status.Warnings) > 0 {
		a.log.Debug("Auditor: encountered warnings writing events %#v", status.Warnings)
	}

	return nil
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
