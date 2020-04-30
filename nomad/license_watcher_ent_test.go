package nomad

import (
	"context"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/nomad/state"

	"github.com/stretchr/testify/require"
)

func newTestLicenseWatcher() *LicenseWatcher {
	logger := hclog.NewInterceptLogger(nil)
	lw, _ := NewLicenseWatcher(logger)
	return lw
}

func TestLicenseWatcher_NotRunningError(t *testing.T) {
	t.Parallel()
	lw := newTestLicenseWatcher()
	require.Equal(t, lw.stop(), ErrLicenseWatcherNotRunning)
}

func TestLicenseWatcher_Running(t *testing.T) {
	t.Parallel()
	lw := newTestLicenseWatcher()
	state := state.TestStateStore(t)
	ctx := context.Background()
	shutdown := func() error { return nil }
	require.NoError(t, lw.start(ctx, state, shutdown))
	require.Error(t, lw.start(ctx, state, shutdown))
}
