// +build ent

package audit

import (
	"context"
	"errors"

	"github.com/hashicorp/go-eventlogger"
	"github.com/hashicorp/go-hclog"
)

func (a *Auditor) NewHTTPFilter(f Filter) (eventlogger.Node, error) {
	// temp stub to return something
	return &HTTPEventFilter{
		// Stages: f.Stage,
	}, nil
}

// Ensure StageFilter is an eventlogger.Node
var _ eventlogger.Node = &HTTPEventFilter{}

// HTTPEventFilter rejects events that match a given stage
type HTTPEventFilter struct {
	Stages     []Stage
	Endpoints  []string
	Operations []string

	log hclog.InterceptLogger
}

// Reopen is used to re-read any config stored externally
// and to close and reopen files, e.g. for log rotation.
func (s *HTTPEventFilter) Reopen() error {
	panic("not implemented") // TODO: Implement
}

// Type describes the type of the node.  This is mostly just used to
// validate that pipelines are sensibly arranged, e.g. ending with a sink.
func (s *HTTPEventFilter) Type() eventlogger.NodeType {
	panic("not implemented") // TODO: Implement
}

// Process does something with the Event: filter, redaction,
// marshalling, persisting.
func (s *HTTPEventFilter) Process(ctx context.Context, e *eventlogger.Event) (*eventlogger.Event, error) {
	event, ok := e.Payload.(*Event)
	if !ok {
		s.log.Error("Payload is not an event after validation step")
		return nil, errors.New("Unprocessable event")
	}

	// Check if we should ignore stage
	for _, stage := range s.Stages {
		if stage.Matches(event.Stage) {
			s.log.Debug("Filtering audit event stage %s matched")
		}
	}

	// Check if we should ignore operation
	// for _, operation := range s.Operations {

	// }

	// Check if we should ignore endpoint

	return nil, nil
}
