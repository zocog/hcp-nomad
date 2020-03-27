// +build ent

package audit

import (
	"context"
	"testing"

	"github.com/hashicorp/go-eventlogger"
	"github.com/stretchr/testify/require"
)

func TestHealthCheckFilter(t *testing.T) {
	f := &HealthCheckFilter{}

	e := testEvent(AuditEvent, OperationReceived)
	e.Request.Endpoint = "/v1/agent/health"

	payload := eventlogger.Event{
		Payload: e,
	}

	event, err := f.Process(context.Background(), &payload)
	require.NoError(t, err)
	require.Nil(t, event)

	e.Request.Endpoint = "/v1/jobs"

	payload = eventlogger.Event{
		Payload: e,
	}

	event, err = f.Process(context.Background(), &payload)
	require.NoError(t, err)
	require.NotNil(t, event)
}
