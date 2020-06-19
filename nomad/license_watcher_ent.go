// +build ent

package nomad

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/consul/lib"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-licensing"
	"github.com/hashicorp/go-memdb"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/state"
)

var (
	// Temporary Nomad-Enterprise licenses should operate for six hours
	temporaryLicenseTimeLimit = 6 * time.Hour
)

type LicenseWatcher struct {
	// once ensures watch is only invoked once
	once sync.Once

	// license is the watchers atomically stored license
	license atomic.Value

	watcher *licensing.Watcher

	logMu  sync.Mutex
	logger hclog.Logger

	// logTimes tracks the last time we sent a log message for a feature
	logTimes map[license.Features]time.Time
}

func NewLicenseWatcher(logger hclog.InterceptLogger, cfg *LicenseConfig) (*LicenseWatcher, error) {
	// Configure the setup options for the license watcher
	tmpLicense, opts, err := watcherStartupOpts(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed setting up license watcher options: %w", err)
	}

	nomadTmpLicense, err := license.NewLicense(tmpLicense)
	if err != nil {
		return nil, fmt.Errorf("failed to convert temporary license: %w", err)
	}

	// Create the new watcher with options
	watcher, _, err := licensing.NewWatcher(opts)
	if err != nil {
		return nil, fmt.Errorf("failed creating license watcher: %w", err)
	}

	lw := &LicenseWatcher{
		once:     sync.Once{},
		watcher:  watcher,
		logger:   logger.Named("licensing"),
		logMu:    sync.Mutex{},
		logTimes: make(map[license.Features]time.Time),
	}

	lw.license.Store(nomadTmpLicense)

	return lw, nil
}

func watcherStartupOpts(cfg *LicenseConfig) (*licensing.License, *licensing.WatcherOptions, error) {
	flags := temporaryFlags()
	tempLicense, signed, pubKey, err := licensing.TemporaryLicenseInfo(license.ProductName, flags, temporaryLicenseTimeLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed creating temporary license: %w", err)
	}

	return tempLicense, &licensing.WatcherOptions{
		ProductName:          license.ProductName,
		InitLicense:          signed,
		AdditionalPublicKeys: append(cfg.AdditionalPubKeys, pubKey),
		CallbackFunc:         nil,
	}, nil
}

// start the license watching process in a goroutine. Callers are responsible
// for ensuring it is shut down properly
func (w *LicenseWatcher) start(ctx context.Context, state *state.StateStore, shutdownFunc func() error) {
	w.once.Do(func() {
		go w.watch(ctx, state, shutdownFunc)
	})
}

func (w *LicenseWatcher) watch(ctx context.Context, state *state.StateStore, shutdownFunc func() error) {
	// licenseSet tracks whether or not a permanent license has been set
	var licenseSet bool
	// signed tracks the latest known license watcher signed license blob
	var signed string

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
		if stored != nil && stored.Signed != "" && stored.Signed != signed {
			if _, err := w.watcher.SetLicense(stored.Signed); err != nil {
				w.logger.Error("failed setting license", "error", err)
			} else {
				signed = stored.Signed
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

	// Handle updated license from the watcher
	case lic := <-w.watcher.UpdateCh():
		w.logger.Debug("received update from license manager")

		// Check if watcher has a license and if it needs to be upated
		watcherLicense := w.License()
		if watcherLicense == nil || !watcherLicense.Equal(lic) {
			// Update license
			nomadLicense, err := license.NewLicense(lic)
			if err == nil {
				w.license.Store(nomadLicense)
			} else {
				w.logger.Error("error loading Nomad license", "error", err)
			}
		}

	// Check for licensing errors, primarily expirations.
	case err := <-w.watcher.ErrorCh():
		w.logger.Error("received error from watcher", "error", err)

		// If a permanent license has not been set, we close the server.
		if !licenseSet {
			w.logger.Error("temporary license expired; shutting down server")
			// Call agent shutdown func asyncronously
			go shutdownFunc()
			return
		}
		w.logger.Error("license expired") //TODO: more info

	case warnLicense := <-w.watcher.WarningCh():
		w.logger.Warn("license expiring", "time_left", time.Until(warnLicense.ExpirationTime).Truncate(time.Second))
	case <-watchSetCtx.Done():
	case <-ctx.Done():
		// stop the watcher
		w.watcher.Stop()
	}
}

// ValidateLicense validates that the given blob is a valid go-licensing
// license as well as a valid nomad license
func (w *LicenseWatcher) ValidateLicense(blob string) (*license.License, error) {
	lic, err := w.watcher.ValidateLicense(blob)
	if err != nil {
		return nil, err
	}
	nLic, err := license.NewLicense(lic)
	if err != nil {
		return nil, err
	}
	return nLic, nil
}

func (w *LicenseWatcher) SetLicense(blob string) (*licensing.License, error) {
	return w.watcher.SetLicense(blob)
}

func (w *LicenseWatcher) Features() license.Features {
	lic := w.license.Load().(*license.License)
	if lic == nil {
		return license.FeatureNone
	}

	// check if our local license has expired
	if time.Now().After(lic.TerminationTime) {
		return license.FeatureNone
	}

	return lic.Features
}

func (w *LicenseWatcher) HasFeature(feature license.Features) bool {
	return w.Features().HasFeature(feature)
}

// FeatureCheck determines if the given feature is included in License
// if emitLog is true, a log will only be sent once ever 5 minutes per feature
func (w *LicenseWatcher) FeatureCheck(feature license.Features, emitLog bool) error {
	if w.HasFeature(feature) {
		return nil
	}

	err := fmt.Errorf("Feature %q is unlicensed", feature.String())

	if emitLog {
		// Only send log messages for a missing feature every 5 minutes
		w.logMu.Lock()
		defer w.logMu.Unlock()
		lastTime := w.logTimes[feature]
		now := time.Now()
		if now.Sub(lastTime) > 5*time.Minute {
			w.logger.Warn(err.Error())
			w.logTimes[feature] = now
		}
	}

	return err
}

// License atomically returns the license watchers stored license
func (w *LicenseWatcher) License() *license.License {
	return w.license.Load().(*license.License)
}
