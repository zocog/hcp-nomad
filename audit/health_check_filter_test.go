//go:build ent
// +build ent

package audit

import (
	"context"
	"testing"

	"github.com/hashicorp/eventlogger"
	"github.com/stretchr/testify/require"
)

func TestHealthCheckFilter(t *testing.T) {
	f := &HealthCheckFilter{}

	// Exact match
	e := testEvent(AuditEvent, OperationReceived)
	e.Request.Endpoint = "/v1/agent/health"

	payload := eventlogger.Event{
		Payload: e,
	}

	event, err := f.Process(context.Background(), &payload)
	require.NoError(t, err)
	require.Nil(t, event)

	// Partial Match
	e.Request.Endpoint = "/v1/agent/health?type=server"

	payload = eventlogger.Event{
		Payload: e,
	}

	event2, err := f.Process(context.Background(), &payload)
	require.NoError(t, err)
	require.Nil(t, event2)

	// No match
	e.Request.Endpoint = "/v1/jobs"

	payload = eventlogger.Event{
		Payload: e,
	}

	event3, err := f.Process(context.Background(), &payload)
	require.NoError(t, err)
	require.NotNil(t, event3)
}
