// +build ent

import "time"

type EventType string
type Stage string

const (
	HTTPEvent               = "HTTPEvent"
	OperationReceived Stage = "OperationReceived"
	OperationComplete Stage = "OperationComplete"
)

type Event struct {
	ID        string
	Type      EventType
	Stage     Stage
	Timestamp time.Time
	Version   string
	Auth
	Request
	Response
}

type Auth struct {
	AccessorID string
	Name       string
	Type       string
	Policies   []string
	Global     bool
	CreateTime time.Time
}

type Request struct {
	ID          string
	Operation   string
	Endpoint    string
	Namespace   map[string]string
	RequestMeta map[string]string
	NodeMeta    map[string]string
}

type Response struct {
	StatusCode int
	raw        []byte
	Error      string
}
