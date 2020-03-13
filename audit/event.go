// +build ent

package audit

import (
	"context"
	"time"

	"github.com/hashicorp/go-eventlogger"
)

const (
	OperationReceived Stage = "OperationReceived"
	OperationComplete Stage = "OperationComplete"
	AllStages         Stage = "*"
)

type Stage string
type HTTPOperation string

// Event represents a audit log entry.
type Event struct {
	ID        string                `json:"id"`
	Stage     Stage                 `json:"stage"`
	Type      eventlogger.EventType `json:"type"`
	Timestamp time.Time             `json:"time_stamp"`
	Version   int                   `json:"version"`
	Auth      `json:"auth"`
	Request   `json:"request"`
	Response  `json:"response"`
}

type Auth struct {
	AccessorID string    `json:"accessor_id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Policies   []string  `json:"policies"`
	Global     bool      `json:"global"`
	CreateTime time.Time `json:"create_time"`
}

type Request struct {
	ID          string            `json:"id"`
	Operation   string            `json:"operation"`
	Endpoint    string            `json:"endpoint"`
	Namespace   map[string]string `json:"namespace"`
	RequestMeta map[string]string `json:"request_meta"`
	NodeMeta    map[string]string `json:"node_meta"`
}

type Response struct {
	StatusCode int    `json:"status_code"`
	Error      string `json:"error"`
	raw        []byte
}

// Matches checks if stage matches a particular stage
// or if either stage is AllStages
func (s Stage) Matches(b Stage) bool {
	return s == b || s == AllStages || b == AllStages
}

func (s Stage) Valid() bool {
	switch s {
	case OperationReceived, OperationComplete, AllStages:
		return true
	default:
		return false
	}
}

func (s Stage) String() string {
	return string(s)
}

type Eventer interface {
	Event(ctx context.Context, s Stage, e Event) error
}
