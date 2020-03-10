// +build ent

package audit

import (
	"time"

	"github.com/hashicorp/go-eventlogger"
)

type RunMode string
type FilterType string

const (
	Enforced   RunMode = "enforced"
	Disabled   RunMode = "false"
	BestEffort RunMode = "best-effort"

	HTTPFilter FilterType = "HTTP"
)

type Auditor struct {
	broker    *eventlogger.Broker
	auditable eventlogger.EventType
}

type Config struct {
	Enabled RunMode
	// FileName is the name that the audit log should follow.
	// If rotation is enabled the pattern will be name-timestamp.log
	FileName string

	// Path where audit log files should be stored.
	Path string

	MaxBytes    int
	MaxDuration time.Duration
	MaxFiles    int
	Filters     []Filter
}

type Filter struct {
	Type      FilterType
	Endpoint  []string
	Stage     []string
	Operation []string
}
