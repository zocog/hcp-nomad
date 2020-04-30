// build +ent

package nomad

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-licensing/license"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"

	"github.com/stretchr/testify/require"
)

func init() {
	builtinPublicKeys = append(builtinPublicKeys, base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey))
}

func newTestLicenseWatcher() *LicenseWatcher {
	logger := hclog.NewInterceptLogger(nil)
	lw, _ := NewLicenseWatcher(logger)
	return lw
}

func testShutdownFunc() error {
	return nil
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
	require.NoError(t, lw.start(ctx, state, testShutdownFunc))
	require.Error(t, lw.start(ctx, state, testShutdownFunc))
}

func TestLicenseWatcher_UpdatingWatcher(t *testing.T) {
	t.Parallel()
	lw := newTestLicenseWatcher()
	state := state.TestStateStore(t)
	ctx := context.Background()
	require.NoError(t, lw.start(ctx, state, testShutdownFunc))
	require.True(t, lw.isRunning, "license watcher should be running")
	initLicense, _ := lw.watcher.License()
	newLicense := license.NewTestLicense()
	stored := &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1000),
	}
	state.UpsertLicense(1000, stored)
	time.Sleep(1 * time.Second)
	fetchedLicense, err := lw.watcher.License()
	require.NoError(t, err)
	require.False(t, fetchedLicense.Equal(initLicense), "fetched license should be different from the inital")
	require.True(t, fetchedLicense.Equal(newLicense.License.License), "fetched license should match the new license")
}
