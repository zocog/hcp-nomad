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

// TestHTTPEventFilter_Proccess tests that different variations of HTTPEventFilter
// correctly filter, or ignore events
func TestHTTPEventFilter_Proccess(t *testing.T) {
	t.Parallel()

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
				Payload: &Event{Request: &Request{Endpoint: "/ui/", Operation: "GET"}},
			},
			f:      &HTTPEventFilter{Endpoints: []string{"*"}, Operations: []string{"GET"}},
			filter: true,
		},
		{
			desc: "filter wildcard operation",
			e: &eventlogger.Event{
				Payload: &Event{Request: &Request{Endpoint: "/ui/", Operation: "POST"}},
			},
			f:      &HTTPEventFilter{Endpoints: []string{"/ui/"}, Operations: []string{"*"}},
			filter: true,
		},
		{
			desc: "filter full endpoint",
			e: &eventlogger.Event{
				Payload: &Event{
					Request: &Request{
						Endpoint:  "/v1/job/ed344e0a-7290-d117-41d3-a64f853ca3c2/allocations",
						Operation: "GET",
					},
				},
			},
			f:      &HTTPEventFilter{Operations: []string{"GET"}, Endpoints: []string{"/v1/job/ed344e0a-7290-d117-41d3-a64f853ca3c2/allocations"}},
			filter: true,
		},
		{
			desc: "filter globbed endpoint",
			e: &eventlogger.Event{
				Payload: &Event{
					Stage: OperationReceived,
					Request: &Request{
						Endpoint: "/v1/job/ed344e0a-7290-d117-41d3-a64f853ca3c2/allocations",
					},
				},
			},
			f:      &HTTPEventFilter{Stages: []Stage{OperationReceived}, Endpoints: []string{"/v1/job/*/allocations"}},
			filter: true,
		},
		{
			desc: "filter wildcard",
			e: &eventlogger.Event{
				Payload: &Event{
					Stage: OperationReceived,
					Request: &Request{
						Endpoint: "/v1/job/ed344e0a-7290-d117-41d3-a64f853ca3c2/allocations",
					},
				},
			},
			f:      &HTTPEventFilter{Stages: []Stage{OperationReceived}, Endpoints: []string{"*"}},
			filter: true,
		},
		{
			desc: "filter query params",
			e: &eventlogger.Event{
				Payload: &Event{
					Stage: OperationReceived,
					Request: &Request{
						Endpoint: "/v1/jobs?prefix=foo#fragment",
					},
				},
			},
			f:      &HTTPEventFilter{Stages: []Stage{OperationReceived}, Endpoints: []string{"/v1/jobs"}},
			filter: true,
		},
		{
			desc: "glob with query params",
			e: &eventlogger.Event{
				Payload: &Event{
					Stage: OperationReceived,
					Request: &Request{
						Endpoint: "/v1/job/job-id/allocations?all=true",
					},
				},
			},
			f:      &HTTPEventFilter{Stages: []Stage{OperationReceived}, Endpoints: []string{"/v1/job/*/allocations"}},
			filter: true,
		},
		{
			desc: "invalid query params and fragments",
			e: &eventlogger.Event{
				Payload: &Event{
					Stage: OperationReceived,
					Request: &Request{
						// Use some examples from
						// https://github.com/golang/go/blob/master/src/net/url/url_test.go
						Endpoint: "/v1/job/job-id/allocations?all=true;x=1/y&#!$&%27()*+,;=",
					},
				},
			},
			f:      &HTTPEventFilter{Stages: []Stage{OperationReceived}, Endpoints: []string{"/v1/job/*/allocations"}},
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
