// +build ent

package audit

import (
	"context"
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
}

type Config struct {
	Enabled RunMode

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
	Type      FilterType
	Endpoint  []string
	Stage     []string
	Operation []string
}

func NewAuditBroker(cfg Config) (*Auditor, error) {
	jsonfmtID := eventlogger.NodeID(uuid.Generate())
	fmtNode := &eventlogger.JSONFormatter{}

	sinkID := eventlogger.NodeID(uuid.Generate())
	sink := &eventlogger.FileSink{
		Path:        cfg.Path,
		FileName:    cfg.FileName,
		Mode:        cfg.Mode,
		MaxBytes:    cfg.MaxBytes,
		MaxDuration: cfg.MaxDuration,
		MaxFiles:    cfg.MaxFiles,
	}

	broker := eventlogger.NewBroker()
	err := broker.RegisterNode(jsonfmtID, fmtNode)
	if err != nil {
		return nil, err
	}

	err = broker.RegisterNode(sinkID, sink)
	if err != nil {
		return nil, err
	}

	err = broker.RegisterPipeline(eventlogger.Pipeline{
		EventType:  AuditEvent,
		PipelineID: AuditPipeline,
		NodeIDs: []eventlogger.NodeID{
			jsonfmtID,
			sinkID,
		},
	})
	if err != nil {
		return nil, err
	}

	return &Auditor{
		broker: broker,
	}, nil
}

func (a *Auditor) Event(ctx context.Context, s Stage, event Event) error {
	status, err := a.broker.Send(ctx, a.et, event)
	if err != nil {
		return err
	}

	if len(status.Warnings) > 0 {
		a.log.Debug("Auditor: encountered warnings writing events %#v", status.Warnings)
	}

	return nil
}
