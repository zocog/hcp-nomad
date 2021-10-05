//go:build ent
// +build ent

package audit

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/hashicorp/eventlogger"
	"github.com/hashicorp/go-hclog"
	"github.com/ryanuber/go-glob"
)

func NewHTTPFilter(log hclog.InterceptLogger, f FilterConfig) (eventlogger.Node, error) {
	// Generate and ensure stages are valid
	var stages []Stage
	for _, s := range f.Stages {
		stage := Stage(s)
		if !stage.Valid() {
			return nil, fmt.Errorf("Unknown stage %s", s)
		}
		stages = append(stages, stage)
	}

	// Canonicalize operations
	var operations []string
	for _, op := range f.Operations {
		operations = append(operations, strings.ToUpper(op))
	}

	return &HTTPEventFilter{
		Stages:     stages,
		Endpoints:  f.Endpoints,
		Operations: operations,
		log:        log,
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

	// Iterate over Endpoints that are potentially filtered
	for _, pattern := range s.Endpoints {
		if s.endpointMatches(pattern, event.Request.Endpoint) {
			// Check if we should ignore stage for matching endpoint
			for _, stage := range s.Stages {
				if stage.Matches(event.Stage) {
					s.log.Trace("Filtering audit event matched", "pattern", pattern, "stage", stage)
					// Return nil to signal that the event should be discarded.
					return nil, nil
				}
			}

			// Check if we should ignore operation for matching endpoint
			for _, operation := range s.Operations {
				if operation == "*" || operation == event.Request.Operation {
					s.log.Trace("Filtering audit event matched", "pattern", pattern, "operation", operation)
					// Return nil to signal that the event should be discarded.
					return nil, nil
				}
			}

			// No filtering to be done, requires endpoint + stage or operation
		}
	}

	// No filtering to be done, return event
	return e, nil
}

func (s *HTTPEventFilter) endpointMatches(pattern, requestURL string) bool {
	operation := requestURL
	// Remove any query params from URL
	if operationPath, err := url.Parse(requestURL); err == nil {
		operation = operationPath.Path
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
