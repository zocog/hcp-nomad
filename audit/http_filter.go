// +build ent

package audit

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/go-eventlogger"
	"github.com/hashicorp/go-hclog"
	"github.com/ryanuber/go-glob"
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
	return nil
}

// Type describes the type of the node.  This is mostly just used to
// validate that pipelines are sensibly arranged, e.g. ending with a sink.
func (s *HTTPEventFilter) Type() eventlogger.NodeType {
	return eventlogger.NodeTypeFilter
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
			// Return nil to signal that the event should be discarded.
			return nil, nil
		}
	}

	// Check if we should ignore operation
	for _, operation := range s.Operations {
		if operation == "*" || strings.ToUpper(operation) == event.Request.Operation {
			// Return nil to signal that the event should be discarded.
			return nil, nil
		}
	}

	// Check if we should ignore endpoint
	for _, pattern := range s.Endpoints {
		if endpointMatches(pattern, event.Request.Endpoint) {
			// Return nil to signal that the event should be discarded.
			return nil, nil
		}
	}

	// No filtering to be done, return event
	return e, nil
}

func endpointMatches(pattern, operation string) bool {
	// all operations
	if pattern == "*" {
		return true
	}

	// exact match
	if pattern == operation {
		return true
	}

	// partial matching using glob syntax
	if glob.Glob(pattern, operation) {
		return true
	}

	return false
}
