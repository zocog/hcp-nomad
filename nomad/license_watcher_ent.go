// +build ent

package nomad

import (
	"context"
	"fmt"
	"io/ioutil"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-licensing"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/state"
)

var (
	// Temporary Nomad-Enterprise licenses should operate for six hours
	temporaryLicenseTimeLimit = 6 * time.Hour

	// expiredTmpGrace is the grace period after a server is restarted with an expired
	// license to allow for a new valid license to be applied.
	// The value should be short enough to inconvenience unlicensed users,
	// but long enough to allow legitimate users to apply the new license
	// manually, e.g. upon upgrades from non-licensed versions.
	// This value gets evaluated twice to be extra cautious
	defaultExpiredTmpGrace = 1 * time.Minute

	permanentLicenseID = "permanent"
)

type stateStoreFn func() *state.StateStore

// LicenseConfig allows for tunable licensing config
// primarily used for enterprise testing
type LicenseConfig struct {
	LicenseFileEnv  string
	LicenseFilePath string

	AdditionalPubKeys []string

	PropagateFn func(*license.License, string) error

	// preventStart is used for testing to control when to start watcher
	preventStart bool
}

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

	establishTmpLicenseBarrierFn func() (int64, error)

	stateStoreFn stateStoreFn

	propagateFn func(*license.License, string) error
}

func NewLicenseWatcher(
	logger hclog.InterceptLogger,
	cfg *LicenseConfig,
	shutdownCallback func() error,
	tmpLicenseBarrierFn func() (int64, error),
	stateStoreFn stateStoreFn,
) (*LicenseWatcher, error) {

	// Check for file license
	var initLicense string
	if cfg.LicenseFileEnv != "" {
		initLicense = cfg.LicenseFileEnv
	} else if cfg.LicenseFilePath != "" {
		licRaw, err := ioutil.ReadFile(cfg.LicenseFilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read license file %w", err)
		}
		initLicense = string(licRaw)
	}

	_, tmpSigned, tmpPubKey, err := temporaryLicenseInfo()
	if err != nil {
		return nil, fmt.Errorf("failed creating temporary license: %w", err)
	}
	cfg.AdditionalPubKeys = append(cfg.AdditionalPubKeys, tmpPubKey)

	// If initLicense was not set by a file license, start with a temporary license
	if initLicense == "" {
		initLicense = tmpSigned
	}

	ctx, cancel := context.WithCancel(context.Background())

	lw := &LicenseWatcher{
		shutdownCallback:             shutdownCallback,
		expiredTmpGrace:              defaultExpiredTmpGrace,
		monitorExpTmpCtx:             ctx,
		monitorExpTmpCancel:          cancel,
		logger:                       logger.Named("licensing"),
		logTimes:                     make(map[license.Features]time.Time),
		establishTmpLicenseBarrierFn: tmpLicenseBarrierFn,
		propagateFn:                  cfg.PropagateFn,
		stateStoreFn:                 stateStoreFn,
	}

	opts := &licensing.WatcherOptions{
		ProductName:          license.ProductName,
		InitLicense:          initLicense,
		AdditionalPublicKeys: cfg.AdditionalPubKeys,
		CallbackFunc:         lw.watcherCallback,
	}

	// Create the new watcher with options
	watcher, _, err := licensing.NewWatcher(opts)
	if err != nil {
		return nil, fmt.Errorf("failed creating license watcher: %w", err)
	}
	lw.watcher = watcher

	startUpLicense, err := lw.watcher.License()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve startup license: %w", err)
	}

	nomadLicense, err := license.NewLicense(startUpLicense)
	if err != nil {
		return nil, fmt.Errorf("failed to convert license: %w", err)
	}

	lw.license.Store(nomadLicense)

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
	tempLicense, licenseBytes, pubKey, err := temporaryLicenseInfo()
	if err != nil {
		return nil, nil, fmt.Errorf("failed creating temporary license: %w", err)
	}

	return tempLicense, &licensing.WatcherOptions{
		ProductName:          license.ProductName,
		InitLicense:          licenseBytes,
		AdditionalPublicKeys: append(cfg.AdditionalPubKeys, pubKey),
		CallbackFunc:         nil,
	}, nil
}

// start the license watching process in a goroutine. Callers are responsible
// for ensuring it is shut down properly
func (w *LicenseWatcher) start(ctx context.Context) {
	w.once.Do(func() {
		go w.watch(ctx)
		go w.temporaryLicenseMonitor(ctx)
	})
}

func (w *LicenseWatcher) watch(ctx context.Context) {
	state := w.stateStoreFn()
	stateAbandonCh := state.AbandonCh()

	// paidLicense indicates if a valid, non-temporary license has been set.
	// If true, agent should not be shutdown.
	var paidLicense bool

	// signed tracks the latest known license watcher signed license blob
	// var signed string

	for {
		// Check if we should exit
		select {
		case <-ctx.Done():
			w.watcher.Stop()
			return
		case <-stateAbandonCh:
			w.logger.Debug("current state abandoned by newer one, getting latest state")
			state = w.stateStoreFn()
			stateAbandonCh = state.AbandonCh()
		default:
		}

		// Add the current state's abandonCh to watchset to signal if the given
		// state store has been abandoned.
		// watchSet := memdb.NewWatchSet()
		// watchSet.Add(stateAbandonCh)

		// stored, err := state.License(watchSet)
		// if err != nil {
		// 	w.logger.Error("failed fetching license from state store", "error", err)
		// 	time.Sleep(lib.RandomStagger(1 * time.Second))
		// 	continue
		// }
		// if stored != nil && stored.Signed != "" && stored.Signed != signed {
		// 	paidLicense = true
		// 	signed = stored.Signed
		// 	if _, err := w.watcher.SetLicense(stored.Signed); err != nil {
		// 		w.logger.Error("failed setting license", "error", err)
		// 	}
		// }
		// TODO(drew) check license.temporary instead
		// TODO test
		// if !paidLicense && !w.License().Temporary {
		// 	paidLicense = true
		// }
		// if !paidLicense && tempLicenseTooOld(tmpLicenseInitTime) {
		// 	// The server is not brand new, the cluster age is too old
		// 	// for a temporary license, track that it is replaced soon
		// 	w.monitorExpiredTmpLicense(ctx)
		// }

		select {
		// Handle updated license from the watcher
		case lic := <-w.watcher.UpdateCh():
			// TODO this is probably where we should update raft instead
			w.logger.Info("received update from license manager")

			// Check if watcher has a license and if it needs to be updated
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
		case <-ctx.Done():
		}
	}
}

func (w *LicenseWatcher) temporaryLicenseMonitor(ctx context.Context) {
	state := w.stateStoreFn()

	// Block for the temporary license barrier to be populated to raft
	// This value is used to check if a temporary license is within
	// the initial 6 hour evaluation period
	tmpLicenseBarrier := w.getOrSetTmpLicenseBarrier(ctx, state)
	w.logger.Info("received temporary license barrier", "init time", tmpLicenseBarrier)

	for {
		tmpLicenseTooOld := time.Now().After(tmpLicenseBarrier.Add(temporaryLicenseTimeLimit))
		license := w.License()

		// Paid license, stop temporary license monitor
		if license != nil && !license.Temporary {
			return
		}

		if license != nil && license.Temporary && tmpLicenseTooOld {
			w.monitorExpiredTmpLicense(ctx)
		}

		exp := tmpLicenseBarrier.Add(temporaryLicenseTimeLimit)
		select {
		case <-time.After(exp.Sub(time.Now())):
			continue
		case <-ctx.Done():
			return
		}
	}

}

func (w *LicenseWatcher) monitorExpiredTmpLicense(ctx context.Context) {
	w.logger.Warn("temporary license too old for evaluation period, beginning expired enterprise evaluation monitor")
	// Grace period for server and raft to initialize
	select {
	case <-ctx.Done():
		return
	case <-w.monitorExpTmpCtx.Done():
		w.logger.Info("received license update, stopping temporary license monitor")
		return
	case <-time.After(w.expiredTmpGrace):
		// A license was never applied
		if w.License().Temporary {
			w.logger.Error("cluster age is greater than temporary license lifespan. Please apply a valid license")
			w.logger.Error("cluster will shutdown soon. Please apply a valid license")
		} else {
			// This shouldn't happen but being careful to prevent bad shutdown
			w.logger.Error("never received monitor ctx cancellation update but license is no longer temporary")
			return
		}
	}

	// Wait once more for valid license to be applied before shutting down
	select {
	case <-w.monitorExpTmpCtx.Done():
		w.logger.Info("license applied, cancelling expired temporary license shutdown")
	case <-time.After(w.expiredTmpGrace):
		w.logger.Error("temporary license grace period expired. shutting down")
		go w.shutdownCallback()
		return
	}
}

func (w *LicenseWatcher) getOrSetTmpLicenseBarrier(ctx context.Context, state *state.StateStore) time.Time {
	interval := time.After(0 * time.Second)
	for {
		select {
		case <-ctx.Done():
			w.logger.Warn("context cancelled while waiting for temporary license barrier")
			return time.Now()
		case err := <-w.watcher.ErrorCh():
			w.logger.Warn("received error from license watcher", "err", err)
			return time.Now()
		case <-interval:
			tmpCreateTime, err := w.establishTmpLicenseBarrierFn()
			if err != nil {
				w.logger.Info("failed to get or set temporary license barrier, retrying...", "error", err)
				interval = time.After(2 * time.Second)
				continue
			}
			return time.Unix(0, tmpCreateTime)
		}
	}
}

func (w *LicenseWatcher) watcherCallback(lic *licensing.License, signed string) error {
	nomadLicense, err := license.NewLicense(lic)
	if err != nil {
		return err
	}

	if !nomadLicense.Temporary && w.propagateFn != nil {
		return w.propagateFn(nomadLicense, signed)
	}

	// TODO(drew) consul sets signed.Store here

	return nil
}

// tempLicenseTooOld checks if the cluster age is older than the temporary
// license time limit
func tmpLicenseTooOld(originalTmpCreate time.Time) bool {
	return time.Now().After(originalTmpCreate.Add(temporaryLicenseTimeLimit))
}

//

// Server Starts Up no previous temporary license barrier in raft
// - checks for license in raft

// Server starts up with previous temporary license
// - tmp license expired
// - tmp license

// Server starts up with no license file on disk
// - no raft license
// - raft license

// Server starts up with license file on disk
// - file license has expired
// - file license is valid

// - no raft license

// - raft license
//   - Take whichever has a longer expiration

// After some time a license appears in Raft

// I have 3 servers with some old license files
// - I put license with feature/module
// Some server X gets restarted, still expect latest license to be used
