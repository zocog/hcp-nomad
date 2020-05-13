// build +ent

package nomad

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-licensing/license"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"

	"github.com/stretchr/testify/require"
)

func newTestLicenseWatcher() *LicenseWatcher {
	logger := hclog.NewInterceptLogger(nil)
	lw, _ := NewLicenseWatcher(logger)
	return lw
}

func testShutdownFunc() error {
	return nil
}

func TestLicenseWatcher_UpdatingWatcher(t *testing.T) {
	t.Parallel()
	TestValidationHelper(t)

	ctx := context.Background()
	lw := newTestLicenseWatcher()
	state := state.TestStateStore(t)
	lw.start(ctx, state, testShutdownFunc)
	initLicense, _ := lw.watcher.License()
	newLicense := license.NewTestLicense(nomadLicense.TestGovernancePolicyFlags())
	stored := &structs.StoredLicense{
		Signed:      newLicense.Signed,
		CreateIndex: uint64(1000),
	}
	state.UpsertLicense(1000, stored)
	time.Sleep(1 * time.Second)
	fetchedLicense, err := lw.watcher.License()
	require.NoError(t, err)
	require.False(t, fetchedLicense.Equal(initLicense), "fetched license should be different from the inital")
	require.True(t, fetchedLicense.Equal(newLicense.License.License), fmt.Sprintf("got: %s wanted: %s", fetchedLicense, newLicense.License.License))
}
