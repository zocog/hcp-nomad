//go:build ent
// +build ent

package audit

import (
	"context"
	"strings"

	"github.com/hashicorp/eventlogger"
)

// HealthCheckFilter filters out agent health check requests
type HealthCheckFilter struct{}

const healthcheck = "/v1/agent/health"

// Process filters out healthcheck requests.
func (h *HealthCheckFilter) Process(ctx context.Context, e *eventlogger.Event) (*eventlogger.Event, error) {
	event, _ := e.Payload.(*Event)

	if strings.HasPrefix(event.Request.Endpoint, healthcheck) {
		return nil, nil
	}
	return e, nil
}

// Reopen is used to re-read any config stored externally
// and to close and reopen files, e.g. for log rotation.
func (h *HealthCheckFilter) Reopen() error {
	return nil
}

// Type describes the type of the node.  This is mostly just used to
// validate that pipelines are sensibly arranged, e.g. ending with a sink.
func (h *HealthCheckFilter) Type() eventlogger.NodeType {
	return eventlogger.NodeTypeFilter
}
