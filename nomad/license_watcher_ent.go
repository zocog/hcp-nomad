// +build ent

package nomad

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/consul/lib"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-licensing"
	"github.com/hashicorp/go-memdb"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/state"
)

var (
	builtinPublicKeys = []string{}

	// Temporary Nomad-Enterprise licenses should operate for six hours
	temporaryLicenseTimeLimit = 6 * time.Hour
)

type LicenseWatcher struct {
	once sync.Once

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
		once:    sync.Once{},
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

func (w *LicenseWatcher) start(ctx context.Context, state *state.StateStore, shutdownFunc func() error) {
	w.once.Do(func() {
		go w.watch(ctx, state, shutdownFunc)
	})
}

func (w *LicenseWatcher) watch(ctx context.Context, state *state.StateStore, shutdownFunc func() error) {
	// licenseSet tracks whether or not a permanent license has been set
	var licenseSet bool

	for {
		// Check if we should exit
		select {
		case <-ctx.Done():
			return
		default:
		}

		watchSet := memdb.NewWatchSet()
		stored, err := state.License(watchSet)
		if err != nil {
			w.logger.Error("failed fetching license from state store", "error", err)
			time.Sleep(lib.RandomStagger(1 * time.Second))
			continue
		}
		if stored != nil && stored.Signed != "" {
			if _, err := w.watcher.SetLicense(stored.Signed); err != nil {
				w.logger.Error("failed setting license", "error", err)
			} else {
				licenseSet = true
			}
		}

		w.watchSet(ctx, watchSet, licenseSet, shutdownFunc)
	}
}

func (w *LicenseWatcher) watchSet(ctx context.Context, watchSet memdb.WatchSet, licenseSet bool, shutdownFunc func() error) {
	// Create a context and cancelFunc scoped to the watchSet
	watchSetCtx, watchSetCancel := context.WithCancel(ctx)
	watchSetCh := watchSet.WatchCh(watchSetCtx)
	defer watchSetCancel()

	select {
	// If watchSetCh returns a nil error, there is a new license. If it returns an actual error,
	// the context is canceled, and the function can exit.
	case err := <-watchSetCh:
		if err == nil {
			w.logger.Debug("retreiving new license")
		} else {
			w.logger.Error("received from license watchset", "error", err)
		}
	// This final case checks for licensing errors, primarily expirations.
	case err := <-w.watcher.ErrorCh():
		w.logger.Error("received error from watcher", "error", err)

		// If a permanent license has not been set, we close the server.
		if !licenseSet {
			w.logger.Error("temporary license expired; shutting down server")
			shutdownFunc()
		}
		w.logger.Error("license expired") //TODO: more info
	case warnLicense := <-w.watcher.WarningCh():
		w.logger.Warn("license expiring", "time_left", time.Until(warnLicense.ExpirationTime).Truncate(time.Second))
	case <-watchSetCtx.Done():
	}
}

func (w *LicenseWatcher) ValidateLicense(blob string) (*licensing.License, error) {
	return w.watcher.ValidateLicense(blob)
}

func (w *LicenseWatcher) SetLicense(blob string) (*licensing.License, error) {
	return w.watcher.SetLicense(blob)
}

func (w *LicenseWatcher) GetLicense() (*nomadLicense.License, error) {
	l, err := w.watcher.License()
	if err != nil {
		return nil, err
	}

	n, err := nomadLicense.NewLicense(l)
	if err != nil {
		return nil, err
	}

	return n, nil
}
