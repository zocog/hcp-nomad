// +build ent

package audit

import (
	"context"
	"testing"

	"github.com/hashicorp/go-eventlogger"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/stretchr/testify/require"
)

func TestHTTPEventFilter_Proccess(t *testing.T) {
	cases := []struct {
		desc   string
		f      *HTTPEventFilter
		e      *eventlogger.Event
		err    bool
		filter bool
	}{
		{
			desc: "eventlogger Event not Audit Event",
			e:    &eventlogger.Event{},
			f:    &HTTPEventFilter{},
			err:  true,
		},
		{
			desc: "event not filtered",
			e:    &eventlogger.Event{Payload: &Event{}},
			f:    &HTTPEventFilter{},
		},
		{
			desc: "filter operation",
			e: &eventlogger.Event{
				Payload: &Event{Request: Request{Operation: "GET"}},
			},
			f:      &HTTPEventFilter{Operations: []string{"get"}},
			filter: true,
		},
		{
			desc: "filter wildcard operation",
			e: &eventlogger.Event{
				Payload: &Event{Request: Request{Operation: "POST"}},
			},
			f:      &HTTPEventFilter{Operations: []string{"*"}},
			filter: true,
		},
		{
			desc: "filter full endpoint",
			e: &eventlogger.Event{
				Payload: &Event{
					Request: Request{
						Endpoint: "/v1/job/ed344e0a-7290-d117-41d3-a64f853ca3c2/allocations",
					},
				},
			},
			f:      &HTTPEventFilter{Endpoints: []string{"/v1/job/ed344e0a-7290-d117-41d3-a64f853ca3c2/allocations"}},
			filter: true,
		},
		{
			desc: "filter globbed endpoint",
			e: &eventlogger.Event{
				Payload: &Event{
					Request: Request{
						Endpoint: "/v1/job/ed344e0a-7290-d117-41d3-a64f853ca3c2/allocations",
					},
				},
			},
			f:      &HTTPEventFilter{Endpoints: []string{"/v1/job/*/allocations"}},
			filter: true,
		},
		{
			desc: "filter wildcard",
			e: &eventlogger.Event{
				Payload: &Event{
					Request: Request{
						Endpoint: "/v1/job/ed344e0a-7290-d117-41d3-a64f853ca3c2/allocations",
					},
				},
			},
			f:      &HTTPEventFilter{Endpoints: []string{"*"}},
			filter: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			tc.f.log = testlog.HCLogger(t)

			event, err := tc.f.Process(context.Background(), tc.e)
			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tc.filter {
				require.Nil(t, event)
			}
		})
	}
}
