//go:build ent
// +build ent

package audit

import (
	"context"
	"testing"

	"github.com/hashicorp/eventlogger"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/stretchr/testify/require"
)

func TestValidator(t *testing.T) {
	t.Parallel()

	v := &Validator{
		log: testlog.HCLogger(t),
	}

	eventGood := &eventlogger.Event{
		Payload: &Event{},
	}

	e, err := v.Process(context.Background(), eventGood)
	require.NoError(t, err)
	require.Equal(t, eventGood, e)

	eventBad := &eventlogger.Event{Payload: []byte("bad")}

	e, err = v.Process(context.Background(), eventBad)
	require.Error(t, err)
	require.Nil(t, e)
}
