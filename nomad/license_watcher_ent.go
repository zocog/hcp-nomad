// +build ent

package nomad

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-licensing"
	"github.com/hashicorp/go-memdb"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/state"
)

var (
	ErrLicenseWatcherRunning    = errors.New("license watcher is already running")
	ErrLicenseWatcherNotRunning = errors.New("license watcher is not running")

	builtinPublicKeys = []string{}
	// Temporary Nomad-Enterprise licenses should operate for six hours
	temporaryLicenseTimeLimit = time.Hour * 6
)

type LicenseWatcher struct {
	// Lock for background goroutine
	mu sync.Mutex
	// RWLock for the status of isRunning
	runMu      sync.RWMutex
	isRunning  bool
	cancelFunc context.CancelFunc

	watcher *licensing.Watcher
	logger  hclog.Logger
}

func NewLicenseWatcher(logger hclog.InterceptLogger) (*LicenseWatcher, error) {
	// Configure the setup options for the license watcher
	opts, err := watcherStartupOpts()
	if err != nil {
		return nil, fmt.Errorf("failed setting up license watcher options: %w", err)
	}

	// Create the new watcher with options
	watcher, _, err := licensing.NewWatcher(opts)
	if err != nil {
		return nil, fmt.Errorf("failed creating license watcher: %w", err)
	}

	return &LicenseWatcher{
		mu:      sync.Mutex{},
		watcher: watcher,
		logger:  logger.Named("licensing"),
	}, nil
}

func watcherStartupOpts() (*licensing.WatcherOptions, error) {
	flags := temporaryFlags()
	tempLicense, pubKey, err := licensing.TemporaryLicense(nomadLicense.ProductName, flags, temporaryLicenseTimeLimit)
	if err != nil {
		return nil, fmt.Errorf("failed creating temporary license: %w", err)
	}

	return &licensing.WatcherOptions{
		ProductName:          nomadLicense.ProductName,
		InitLicense:          tempLicense,
		AdditionalPublicKeys: append(builtinPublicKeys, pubKey),
		CallbackFunc:         licenseValidator,
	}, nil
}

func (w *LicenseWatcher) start(ctx context.Context, state *state.StateStore, shutdownFunc func() error) error {
	if w.IsRunning() {
		return ErrLicenseWatcherRunning
	}

	w.runMu.Lock()
	defer w.runMu.Unlock()
	w.isRunning = true

	// Create a "start"-scoped context with a cancel func that both the parent context and stop() can cancel
	w.mu.Lock()
	watcherCtx, watcherCancel := context.WithCancel(ctx)
	w.cancelFunc = watcherCancel

	go func() {
		defer func() {
			w.runMu.Lock()
			w.isRunning = false
			w.runMu.Unlock()
		}()
		defer w.mu.Unlock()

		// licenseSet tracks whether or not a permanent license has been set
		var licenseSet bool

		for {
			watchSet := memdb.NewWatchSet()
			stored, err := state.License(watchSet)
			if err != nil {
				w.logger.Error("failed fetching license from state store", "error", err)
			}
			if stored != nil && stored.Signed != "" {
				if _, err := w.watcher.SetLicense(stored.Signed); err != nil {
					w.logger.Error("failed setting license", "error", err)
				}
				licenseSet = true
			}

			// Create a context and cancelFunc scoped to the watchSet
			watchSetCtx, watchSetCancel := context.WithCancel(context.Background())
			watchSetCh := watchSet.WatchCh(watchSetCtx)

			select {
			// Exit if parent context done or stop is called
			case <-watcherCtx.Done():
				watchSetCancel()
				return
			// If watchSetCh returns a nil error, there is a new license. If it returns an actual error,
			// the context is done or canceled, and the function can exit.
			case err := <-watchSetCh:
				if err != nil {
					w.logger.Debug("retreiving new license")
				}
			// This final case checks for licensing errors, primarily expirations.
			case err := <-w.watcher.ErrorCh():
				w.logger.Error("received error from watcher", "error", err)

				// If a permanent license has not been set, we close the server.
				if !licenseSet {
					w.logger.Error("temporary license expired; shutting down server")
					watchSetCancel()
					shutdownFunc()
					return
				}
				w.logger.Error("license expired") //TODO: more info
			case warnLicense := <-w.watcher.WarningCh():
				w.logger.Warn("license expiring", "time_left", time.Until(warnLicense.ExpirationTime).Truncate(time.Second))
			}
			watchSetCancel()
		}
	}()
	return nil
}

func (w *LicenseWatcher) stop() error {
	if !w.IsRunning() {
		return ErrLicenseWatcherNotRunning
	}
	w.cancelFunc()
	return nil
}

func (w *LicenseWatcher) IsRunning() bool {
	w.runMu.RLock()
	defer w.runMu.RUnlock()
	return w.isRunning
}

func (w *LicenseWatcher) ValidateLicense(blob string) (*licensing.License, error) {
	return w.watcher.ValidateLicense(blob)
}

func (w *LicenseWatcher) SetLicense(blob string) (*licensing.License, error) {
	return w.watcher.SetLicense(blob)
}

// TestValidationHelper must be called before a server is initialized
// It should only be used in tests where a new test license needs to be
// created.
func TestValidationHelper(t *testing.T) {
	builtinPublicKeys = append(builtinPublicKeys, base64.StdEncoding.EncodeToString(nomadLicense.TestPublicKey))
}
