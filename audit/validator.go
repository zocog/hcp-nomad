//go:build ent
// +build ent

package audit

import (
	"context"
	"errors"
	"reflect"

	"github.com/hashicorp/eventlogger"
	"github.com/hashicorp/go-hclog"
)

type Validator struct {
	log hclog.InterceptLogger
}

var _ eventlogger.Node = &Validator{}

func NewValidator(cfg *Config) *Validator {
	return &Validator{
		log: cfg.Logger.NamedIntercept("AuditValidator"),
	}
}

// Process does something with the Event: filter, redaction,
// marshalling, persisting.
func (v *Validator) Process(ctx context.Context, e *eventlogger.Event) (*eventlogger.Event, error) {
	_, ok := e.Payload.(*Event)
	if !ok {
		t := reflect.TypeOf(e.Payload)
		v.log.Error("Auditor: event payload is not an Audit Event", "event type",
			t.Name, "creation time", e.CreatedAt)
		return nil, errors.New("unallowed Event payload")
	}
	return e, nil
}

// Reopen is used to re-read any config stored externally
// and to close and reopen files, e.g. for log rotation.
func (v *Validator) Reopen() error {
	return nil
}

// Type describes the type of the node.  This is mostly just used to
// validate that pipelines are sensibly arranged, e.g. ending with a sink.
func (v *Validator) Type() eventlogger.NodeType {
	return eventlogger.NodeTypeFilter
}

func (v *Validator) Name() string {
	return "AuditValidator"
}
