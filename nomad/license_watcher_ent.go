// +build ent

package nomad

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-licensing"
	"github.com/hashicorp/go-memdb"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/state"
)

var (
	ErrLicenseWatcherRunning    = errors.New("license watcher is already running")
	ErrLicenseWatcherNotRunning = errors.New("license watcher is not running")
)

type LicenseWatcher struct {
	mu        sync.Mutex
	isRunning bool

	watcher *licensing.Watcher
	cancel  context.CancelFunc
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
	tempLicense, pubKey, err := licensing.TemporaryLicense(nomadLicense.ProductName, nil, temporaryLicenseTimeLimit)
	if err != nil {
		return nil, fmt.Errorf("failed creating temporary license: %w", err)
	}
	return &licensing.WatcherOptions{
		ProductName:          nomadLicense.ProductName,
		InitLicense:          tempLicense,
		AdditionalPublicKeys: []string{pubKey},
	}, nil
}

func (w *LicenseWatcher) start(ctx context.Context, state *state.StateStore, shutdownFunc func() error) error {
	if w.isRunning {
		return ErrLicenseWatcherRunning
	}
	w.mu.Lock()
	w.isRunning = true
	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	go func() {
		defer func() { w.isRunning = false }()
		defer w.mu.Unlock()

		// licenseSet tracks whether or not a permanent license has been set
		var licenseSet bool

		// watchSetCh is used to cache the watch set chan for this goroutine
		var watchSetCh <-chan error

		var watchSet memdb.WatchSet

		// watchSetEmitted tracks whether or not the watch set has emitted an error
		// Initially set to true to initialize the watchSet and WatchSetCh
		var watchSetEmitted = true

		for {
			// Create a new watchSetCh if it's our first or the previous has emitted
			if watchSetEmitted {
				watchSet = memdb.NewWatchSet()
				watchSetCh = watchSet.WatchCh(ctx)
				watchSetEmitted = false
			}

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

			select {
			// If watchSetCh returns a nil error, there is a new license. If it returns an actual error,
			// the context is done or canceled, and the function can exit.
			case err := <-watchSetCh:
				watchSetEmitted = true
				if err != nil {
					w.logger.Debug("retreiving new license")
				} else {
					return
				}
			// This final case checks for licensing errors, primarily expirations.
			case err := <-w.watcher.ErrorCh():
				w.logger.Error("received error from watcher", "error", err)

				// If a permanent license has not been set, we close the server.
				if !licenseSet {
					w.logger.Error("temporary license expired; shutting down server")
					shutdownFunc()
					return
				}
				w.logger.Error("license expired") //TODO: more info
			case warnLicense := <-w.watcher.WarningCh():
				w.logger.Warn("license expiring", "time_left", time.Until(warnLicense.ExpirationTime).Truncate(time.Second))
			}
		}
	}()
	return nil
}

func (w *LicenseWatcher) stop() error {
	if !w.isRunning {
		return ErrLicenseWatcherNotRunning
	}
	w.cancel()
	return nil
}

func (w *LicenseWatcher) ValidateLicense(blob string) (*licensing.License, error) {
	return w.watcher.ValidateLicense(blob)
}

func (w *LicenseWatcher) SetLicense(blob string) (*licensing.License, error) {
	return w.watcher.SetLicense(blob)
}
