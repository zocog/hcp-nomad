//go:build ent
// +build ent

package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/eventlogger"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/stretchr/testify/require"
)

type eventWrapper struct {
	CreatedAt time.Time `json:"created_at"`
	EventType string    `json:"event_type"`
	Payload   Event     `json:"payload"`
}

type testChecker struct {
	fail bool
}

func (t *testChecker) FeatureCheck(feature license.Features, emitLog bool) error {
	if t.fail {
		return fmt.Errorf("Feature %q is unlicensed", feature.String())
	}
	return nil
}

// TestAuditor tests we can send an event all the way through the pipeline
// and to a sink, and that we can process the JSON file
func TestAuditor(t *testing.T) {
	t.Parallel()

	// Create a temp directory for the audit log file
	tmpDir, err := ioutil.TempDir("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	auditor, err := NewAuditor(&Config{
		Logger:  testlog.HCLogger(t),
		Enabled: true,
		Filters: []FilterConfig{},
		Sinks: []SinkConfig{
			{
				Name:              "json file",
				Type:              FileSink,
				Format:            JSONFmt,
				DeliveryGuarantee: Enforced,
				FileName:          "audit.log",
				Path:              tmpDir,
			},
		},
		FeatureChecker: &testChecker{},
	})
	require.NoError(t, err)
	require.NotNil(t, auditor)

	e := testEvent(AuditEvent, OperationReceived)

	// Send event
	err = auditor.Event(context.Background(), "audit", e)
	require.NoError(t, err)

	// Read from audit log
	dat, err := ioutil.ReadFile(filepath.Join(tmpDir, "audit.log"))
	require.NoError(t, err)

	// Ensure we can unmarshal back to an event
	var jsonEvent eventWrapper
	err = json.Unmarshal(dat, &jsonEvent)
	require.NoError(t, err)

	require.Equal(t, e.Request.Endpoint, jsonEvent.Payload.Request.Endpoint)
}

// TestAuditor_Rotate tests we can send an event all the way through the
// pipeline and to a sink, and that we can process the JSON file when rotation
// is enabled.
func TestAuditor_Rotate(t *testing.T) {
	t.Parallel()

	// Create a temp directory for the audit log file
	tmpDir := t.TempDir()

	auditor, err := NewAuditor(&Config{
		Logger:  testlog.HCLogger(t),
		Enabled: true,
		Filters: []FilterConfig{},
		Sinks: []SinkConfig{
			{
				Name:              "log file",
				Type:              FileSink,
				Format:            JSONFmt,
				DeliveryGuarantee: Enforced,
				FileName:          "audit.log",
				Path:              tmpDir,
				RotateBytes:       10,
				RotateMaxFiles:    1,
			},
		},
		FeatureChecker: &testChecker{},
	})
	require.NoError(t, err)
	require.NotNil(t, auditor)

	// Send events
	for i := 0; i < 3; i++ {
		e := testEvent(AuditEvent, OperationReceived)
		err = auditor.Event(context.Background(), "audit", e)
		require.NoError(t, err)
	}

	lastEvent := testEvent(AuditEvent, OperationReceived)
	err = auditor.Event(context.Background(), "audit", lastEvent)
	require.NoError(t, err)

	// Ensure only MaxFiles+1 files exist
	files, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	require.Len(t, files, 2)
	found := false
	for _, file := range files {
		if file.Name() == "audit.log" {
			found = true
			break
		}
	}
	require.True(t, found)

	// Read from audit log
	dat, err := ioutil.ReadFile(filepath.Join(tmpDir, "audit.log"))
	require.NoError(t, err)

	// Ensure we can unmarshal back to an event
	var jsonEvent eventWrapper
	err = json.Unmarshal(dat, &jsonEvent)
	require.NoError(t, err)

	require.Equal(t, lastEvent.ID, jsonEvent.Payload.ID)
}

// TestAuditor_NewDir tests a directory that doesn't exist is properly made
// with default permissions.
func TestAuditor_NewDir(t *testing.T) {
	t.Parallel()

	// Create a temp directory for the audit log file
	tmpDir, err := ioutil.TempDir("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	fileMode := fs.FileMode(0o640)
	auditDir := filepath.Join(tmpDir, "audit")
	auditor, err := NewAuditor(&Config{
		Logger:         testlog.HCLogger(t),
		Enabled:        true,
		FeatureChecker: &testChecker{},
		Filters:        []FilterConfig{},
		Sinks: []SinkConfig{
			{
				Name:              "json file",
				Type:              FileSink,
				Format:            JSONFmt,
				DeliveryGuarantee: Enforced,
				FileName:          "audit.log",
				Path:              auditDir,
				Mode:              fileMode,
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, auditor)

	e := testEvent(AuditEvent, OperationReceived)

	// Send event
	err = auditor.Event(context.Background(), "audit", e)
	require.NoError(t, err)

	filename := filepath.Join(auditDir, "audit.log")

	// Ensure directory and file have the expected modes. Expected
	// directory mode is hardcoded in eventlogger.
	dirInfo, err := os.Stat(auditDir)
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o700), dirInfo.Mode().Perm())

	fileInfo, err := os.Stat(filename)
	require.NoError(t, err)
	require.Equal(t, fileMode, fileInfo.Mode())

	// Read from audit log
	dat, err := ioutil.ReadFile(filename)
	require.NoError(t, err)

	// Ensure we can unmarshal back to an event
	var jsonEvent eventWrapper
	err = json.Unmarshal(dat, &jsonEvent)
	require.NoError(t, err)

	require.Equal(t, e.Request.Endpoint, jsonEvent.Payload.Request.Endpoint)
}

// TestAuditor_ExistingDir tests a directory that doest exist is used without
// changing the permissions.
func TestAuditor_ExistingDir(t *testing.T) {
	t.Parallel()

	// Create a temp directory for the audit log file
	tmpDir, err := ioutil.TempDir("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	fileMode := fs.FileMode(0o640)

	// Create the directory and set non-default permissions
	auditDir := filepath.Join(tmpDir, "audit")
	dirMode := fs.FileMode(0o770)
	require.NoError(t, os.Mkdir(auditDir, dirMode))

	auditor, err := NewAuditor(&Config{
		Logger:         testlog.HCLogger(t),
		Enabled:        true,
		FeatureChecker: &testChecker{},
		Filters:        []FilterConfig{},
		Sinks: []SinkConfig{
			{
				Name:              "json file",
				Type:              FileSink,
				Format:            JSONFmt,
				DeliveryGuarantee: Enforced,
				FileName:          "audit.log",
				Path:              auditDir,
				Mode:              fileMode,
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, auditor)

	e := testEvent(AuditEvent, OperationReceived)

	// Send event
	err = auditor.Event(context.Background(), "audit", e)
	require.NoError(t, err)

	fn := filepath.Join(auditDir, "audit.log")

	// Ensure directory and file have the expected modes.
	dirInfo, err := os.Stat(auditDir)
	require.NoError(t, err)
	require.Equal(t, dirMode, dirInfo.Mode().Perm())
	fileInfo, err := os.Stat(fn)
	require.NoError(t, err)
	require.Equal(t, fileMode, fileInfo.Mode())

	// Read from audit log
	dat, err := ioutil.ReadFile(fn)
	require.NoError(t, err)

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
		Logger:         testlog.HCLogger(t),
		Enabled:        true,
		FeatureChecker: &testChecker{},
		Filters: []FilterConfig{
			// filter all stages for endpoints matching /v1/job
			{
				Type:      HTTPEvent,
				Stages:    []string{"*"},
				Endpoints: []string{"/v1/job/*"},
			},
		},
		Sinks: []SinkConfig{
			{
				Name:              "json file",
				Type:              FileSink,
				Format:            JSONFmt,
				DeliveryGuarantee: Enforced,
				FileName:          "audit.log",
				Path:              tmpDir,
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, auditor)

	// configure event to not be filtered
	notFiltered := testEvent(AuditEvent, OperationReceived)
	notFiltered.Request.Endpoint = "/v1/allocations"

	// Send event
	err = auditor.Event(context.Background(), "audit", notFiltered)
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
	err = auditor.Event(context.Background(), "audit", filtered)
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

func TestAuditor_Delivery_BestEffort(t *testing.T) {
	t.Parallel()

	logger := testlog.HCLogger(t)
	// Create a temp directory for the audit log file
	tmpDir, err := ioutil.TempDir("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	auditor, err := NewAuditor(&Config{
		Logger:         logger,
		Enabled:        true,
		Filters:        []FilterConfig{},
		FeatureChecker: &testChecker{},
		Sinks: []SinkConfig{
			{
				Name:   "json file",
				Type:   FileSink,
				Format: JSONFmt,
				// BestEffort
				DeliveryGuarantee: BestEffort,
				FileName:          "audit.log",
				Path:              tmpDir,
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, auditor)

	// Send event that will fail validation
	err = auditor.Event(context.Background(), "audit", []byte("not an event"))
	require.NoError(t, err)

}

func TestAuditor_Unlicensed(t *testing.T) {
	t.Parallel()

	logger := testlog.HCLogger(t)
	// Create a temp directory for the audit log file
	tmpDir, err := ioutil.TempDir("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	auditor, err := NewAuditor(&Config{
		Logger:         logger,
		Enabled:        true,
		Filters:        []FilterConfig{},
		FeatureChecker: &testChecker{fail: true},
		Sinks: []SinkConfig{
			{
				Name:   "json file",
				Type:   FileSink,
				Format: JSONFmt,
				// BestEffort
				DeliveryGuarantee: BestEffort,
				FileName:          "audit.log",
				Path:              tmpDir,
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, auditor)

	// Send event that will fail validation
	err = auditor.Event(context.Background(), "audit", []byte("not an event"))
	require.NoError(t, err)

	// Verify the audit log does not exist
	_, err = os.Stat(filepath.Join(tmpDir, "audit.log"))
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}

func testEvent(et eventlogger.EventType, s Stage) *Event {
	e := &Event{
		ID:        uuid.Generate(),
		Type:      AuditEvent,
		Stage:     s,
		Timestamp: time.Now(),
		Version:   1,
		Auth: &Auth{
			AccessorID: uuid.Generate(),
			Name:       "user@hashicorp.com",
			Policies:   []string{"global"},
			Global:     true,
			CreateTime: time.Now(),
		},
		Request: &Request{
			ID:        uuid.Generate(),
			Operation: "GET",
			Endpoint:  "/v1/job/ed344e0a-7290-d117-41d3-a64f853ca3c2/allocations",
		},
	}

	return e
}
