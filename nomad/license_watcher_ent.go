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

	// expiredTmpGrace is the amount of time we'll allow for a valid license to
	// be applied after a cluster starts up with an expired temporary license
	defaultExpiredTmpGrace = 1 * time.Minute
)

type LicenseWatcher struct {
	// once ensures watch is only invoked once
	once sync.Once

	// license is the watchers atomically stored license
	license atomic.Value

	// shutdownFunc is a callback invoked when temporary license expires and server should shutdown
	shutdownCallback func() error

	// expiredTmpGrace is the duration to allow a valid license to be applied
	// when a server starts with a temporary license and a cluster age greater
	// than the temporaryLicenseTimeLimit
	expiredTmpGrace time.Duration

	// monitorExpTmpCtx is the context used to notify that the expired
	// temporary license monitor should stop
	monitorExpTmpCtx    context.Context
	monitorExpTmpCancel context.CancelFunc

	watcher *licensing.Watcher

	logMu  sync.Mutex
	logger hclog.Logger

	// logTimes tracks the last time we sent a log message for a feature
	logTimes map[license.Features]time.Time
}

func NewLicenseWatcher(logger hclog.InterceptLogger, cfg *LicenseConfig, shutdownCallback func() error) (*LicenseWatcher, error) {
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

	ctx, cancel := context.WithCancel(context.Background())

	lw := &LicenseWatcher{
		watcher:             watcher,
		shutdownCallback:    shutdownCallback,
		expiredTmpGrace:     defaultExpiredTmpGrace,
		monitorExpTmpCtx:    ctx,
		monitorExpTmpCancel: cancel,
		logger:              logger.Named("licensing"),
		logTimes:            make(map[license.Features]time.Time),
	}

	lw.license.Store(nomadTmpLicense)

	return lw, nil
}

// License atomically returns the license watchers stored license
func (w *LicenseWatcher) License() *license.License {
	return w.license.Load().(*license.License)
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

// FeatureCheck determines if the given feature is included in License
// if emitLog is true, a log will only be sent once ever 5 minutes per feature
func (w *LicenseWatcher) FeatureCheck(feature license.Features, emitLog bool) error {
	if w.hasFeature(feature) {
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

func (w *LicenseWatcher) hasFeature(feature license.Features) bool {
	return w.Features().HasFeature(feature)
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
func (w *LicenseWatcher) start(ctx context.Context, state *state.StateStore) {
	w.once.Do(func() {
		go w.watch(ctx, state)
	})
}

func (w *LicenseWatcher) watch(ctx context.Context, state *state.StateStore) {
	// Block for cluster metadata to be populated to raft
	// Then set the watchers clusterCreateTime to check if
	// the temporary license should be allowed or not
	clusterCreateTime := w.clusterStartTime(ctx, state)

	// paidLicense indicates if a valid, non-temporary license has been set.
	// If true, agent should not be shutdown.
	var paidLicense bool

	// signed tracks the latest known license watcher signed license blob
	var signed string

	for {
		// Check if we should exit
		select {
		case <-ctx.Done():
			w.watcher.Stop()
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
			paidLicense = true
			signed = stored.Signed
			if _, err := w.watcher.SetLicense(stored.Signed); err != nil {
				w.logger.Error("failed setting license", "error", err)
			}
		}
		if !paidLicense && clusterTooOldForTmp(clusterCreateTime) {
			// The server is not brand new, the cluster age is too old
			// for a temporary license, track that it is replaced soon
			w.monitorExpiredTmpLicense(ctx)
		}

		w.watchSet(ctx, watchSet, paidLicense)
	}
}

func (w *LicenseWatcher) watchSet(ctx context.Context, watchSet memdb.WatchSet, paidLicense bool) {
	// Create a context and cancelFunc scoped to the watchSet
	wsCtx, wsCancel := context.WithCancel(ctx)
	wsCh := watchSet.WatchCh(wsCtx)
	defer wsCancel()

	select {
	// If watchSetCh returns a nil error, there is a new license. If it returns an actual error,
	// the context is canceled, and the function can exit.
	case err := <-wsCh:
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
				w.monitorExpTmpCancel()
			} else {
				w.logger.Error("error loading Nomad license", "error", err)
			}
		}

	// Check for licensing errors, primarily expirations.
	case err := <-w.watcher.ErrorCh():
		w.logger.Error("received error from watcher", "error", err)

		// If a paid license has not been set, we close the server.
		if !paidLicense {
			w.logger.Error("temporary license expired; shutting down server")
			// Call agent shutdown func asyncronously
			w.watcher.Stop()
			go w.shutdownCallback()
			return
		}
		w.logger.Error("license expired") //TODO: more info

	case warnLicense := <-w.watcher.WarningCh():
		w.logger.Warn("license expiring", "time_left", time.Until(warnLicense.ExpirationTime).Truncate(time.Second))
	case <-wsCtx.Done():
	case <-ctx.Done():
	}
}

func (w *LicenseWatcher) clusterStartTime(ctx context.Context, state *state.StateStore) time.Time {
	ws := memdb.NewWatchSet()
	meta, err := state.ClusterMetadata(ws)
	if err != nil {
		w.logger.Warn("failed to check cluster metadata", "error", err)
	}
	if meta != nil {
		return time.Unix(0, meta.CreateTime).UTC()
	}

	wsCtx, wsCancel := context.WithCancel(ctx)
	defer wsCancel()
	wsCh := ws.WatchCh(wsCtx)
	for {
		select {
		case <-ctx.Done():
			w.logger.Debug("context cancelled while waiting for cluster metadata")
			return time.Now()
		case err := <-w.watcher.ErrorCh():
			// Catch error even if we aren't ready for watcher yet
			// if errorch has no receiver it will panic.
			w.logger.Warn("received error from watcher", "err", err)
		case err := <-wsCh:
			if err == nil {
				clustermeta, err := state.ClusterMetadata(nil)
				if err != nil {
					w.logger.Warn("failed to check cluster metadata", "error", err)
					time.Sleep(lib.RandomStagger(1 * time.Second))
					continue
				}
				return time.Unix(0, clustermeta.CreateTime).UTC()
			}
		}
	}
}

func (w *LicenseWatcher) monitorExpiredTmpLicense(ctx context.Context) {
	go func() {
		// Grace period for server and raft to initialize
		select {
		case <-ctx.Done():
			return
		case <-w.monitorExpTmpCtx.Done():
			w.logger.Debug("received license update, stopping temporary license monitor")
			return
		case <-time.After(w.expiredTmpGrace):
			// A license was never applied
			if w.License().Temporary {
				w.logger.Error("cluster age is greater than temporary license lifespan. Please apply a valid license")
				w.logger.Error("cluster will shutdown soon. Please apply a valid license")
			} else {
				// This shouldn't happen but being careful to prevent bad shutdown
				w.logger.Debug("never received monitor ctx cancellation update but license is no longer temporary")
				return
			}
		}

		// Wait once more for valid license to be applied before shutting down
		select {
		case <-w.monitorExpTmpCtx.Done():
			w.logger.Debug("license applied, cancelling expired temporary license shutdown")
		case <-time.After(w.expiredTmpGrace):
			w.logger.Debug("temporary license grace period expired. shutting down")
			go w.shutdownCallback()
			return
		}
	}()
}

// clusterTooOldForTmp checks if the cluster age is older than the temporary
// license time limit
func clusterTooOldForTmp(clusterCreateTime time.Time) bool {
	return time.Now().After(clusterCreateTime.Add(temporaryLicenseTimeLimit))
}
