// +build ent

package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/go-eventlogger"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/stretchr/testify/require"
)

type eventWrapper struct {
	CreatedAt time.Time `json"created_at"`
	EventType string    `json:"event_type"`
	Payload   Event     `json:"payload"`
}

// TestNewAuditBroker ensures we can create a new AuditBroker without Error
func TestNewAuditor(t *testing.T) {
	t.Parallel()
}

func TestAuditor_Filters(t *testing.T) {
	t.Parallel()
}

func TestAuditor(t *testing.T) {
	t.Parallel()

	tmpDir, err := ioutil.TempDir("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	auditor, err := NewAuditor(&Config{
		Enabled:  true,
		RunMode:  Enforced,
		FileName: "audit.log",
		Path:     tmpDir,
		Filters:  []Filter{},
	})
	require.NoError(t, err)
	require.NotNil(t, auditor)

	e := testEvent(AuditEvent, OperationReceived)

	// Send event
	err = auditor.Event(context.Background(), e)
	require.NoError(t, err)

	// Read from audit log
	dat, err := ioutil.ReadFile(filepath.Join(tmpDir, "audit.log"))
	require.NoError(t, err)

	type eventWrapper struct {
		CreatedAt time.Time `json"created_at"`
		EventType string    `json:"event_type"`
		Payload   Event     `json:"payload"`
	}

	// Ensure we can unmarshal back to an event
	var jsonEvent eventWrapper
	err = json.Unmarshal(dat, &jsonEvent)
	require.NoError(t, err)

	require.Equal(t, e.Request.Endpoint, jsonEvent.Payload.Request.Endpoint)
}

func TestAuditor_Filter(t *testing.T) {
	t.Parallel()

	tmpDir, err := ioutil.TempDir("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	auditor, err := NewAuditor(&Config{
		Logger:   testlog.HCLogger(t),
		Enabled:  true,
		RunMode:  Enforced,
		FileName: "audit.log",
		Path:     tmpDir,
		Filters: []Filter{
			{
				Type:     HTTPEvent,
				Stage:    []string{"*"},
				Endpoint: []string{"/v1/job/*"},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, auditor)

	// configure event to not be filtered
	notFiltered := testEvent(AuditEvent, OperationReceived)
	notFiltered.Request.Endpoint = "/v1/allocations"

	// Send event
	err = auditor.Event(context.Background(), notFiltered)
	require.NoError(t, err)

	// Read from audit log
	file, err := os.Open(filepath.Join(tmpDir, "audit.log"))
	defer file.Close()
	require.NoError(t, err)

	var logs []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		logs = append(logs, scanner.Text())
	}
	require.Len(t, logs, 1)

	// Matching filter endpoint
	filtered := testEvent(AuditEvent, OperationReceived)
	filtered.Request.Endpoint = "/v1/job/job-id/allocations"
	filtered.ID = "filtered-event"

	// Send filtered event
	err = auditor.Event(context.Background(), filtered)
	require.NoError(t, err)

	// Re-Read from audit log
	fileTwo, err := os.Open(filepath.Join(tmpDir, "audit.log"))
	require.NoError(t, err)
	defer fileTwo.Close()

	scanner = bufio.NewScanner(fileTwo)
	logs = nil
	for scanner.Scan() {
		logs = append(logs, scanner.Text())
	}
	require.Len(t, logs, 1)

}

// func TestAuditor_Mode_Enforced(t *testing.T) {
// 	t.Parallel()

// 	tmpDir, err := ioutil.TempDir("", t.Name())
// 	require.NoError(t, err)
// 	defer os.RemoveAll(tmpDir)

// 	auditor, err := NewAuditor(&Config{
// 		Enabled: true,
// 		Mode:    Enforced,
// 	})
// 	require.NoError(t, err)

// }

func testEvent(et eventlogger.EventType, s Stage) *Event {
	e := &Event{
		ID:        uuid.Generate(),
		Type:      AuditEvent,
		Stage:     s,
		Timestamp: time.Now(),
		Version:   1,
		Auth: Auth{
			AccessorID: uuid.Generate(),
			Name:       "user@hashicorp.com",
			Policies:   []string{"global"},
			Global:     true,
			CreateTime: time.Now(),
		},
		Request: Request{
			ID:        uuid.Generate(),
			Operation: "GET",
			Endpoint:  "/v1/job/ed344e0a-7290-d117-41d3-a64f853ca3c2/allocations",
		},
	}

	return e
}
