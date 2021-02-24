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
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"
	"golang.org/x/time/rate"
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

type LicenseInfo struct {
	License *nomadLicense.License
	Signed  string
	Source  string
}

// LicenseConfig allows for tunable licensing config
// primarily used for enterprise testing
type LicenseConfig struct {
	LicenseFileEnv  string
	LicenseFilePath string

	AdditionalPubKeys []string

	PropagateFn func(*nomadLicense.License, string) error

	// preventStart is used for testing to control when to start watcher
	preventStart bool
}

type LicenseWatcher struct {
	// once ensures watch is only invoked once
	once sync.Once

	// license is the watchers atomically stored license
	license atomic.Value

	licenseSource string

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
	logTimes map[nomadLicense.Features]time.Time

	establishTmpLicenseBarrierFn func() (int64, error)

	stateStoreFn stateStoreFn

	propagateFn func(*nomadLicense.License, string) error
}

func NewLicenseWatcher(
	logger hclog.InterceptLogger,
	cfg *LicenseConfig,
	shutdownCallback func() error,
	tmpLicenseBarrierFn func() (int64, error),
	stateStoreFn stateStoreFn,
) (*LicenseWatcher, error) {

	// Check for file license
	initLicense, err := licenseFromLicenseConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to read license from config: %w", err)
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
		logTimes:                     make(map[nomadLicense.Features]time.Time),
		establishTmpLicenseBarrierFn: tmpLicenseBarrierFn,
		propagateFn:                  cfg.PropagateFn,
		stateStoreFn:                 stateStoreFn,
	}

	opts := &licensing.WatcherOptions{
		ProductName:          nomadLicense.ProductName,
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

	license, err := nomadLicense.NewLicense(startUpLicense)
	if err != nil {
		return nil, fmt.Errorf("failed to convert license: %w", err)
	}

	lw.license.Store(license)

	return lw, nil
}

func licenseFromLicenseConfig(cfg *LicenseConfig) (string, error) {
	if cfg.LicenseFileEnv != "" {
		return cfg.LicenseFileEnv, nil
	}

	if cfg.LicenseFilePath != "" {
		licRaw, err := ioutil.ReadFile(cfg.LicenseFilePath)
		if err != nil {
			return "", fmt.Errorf("failed to read license file %w", err)
		}
		return string(licRaw), nil
	}

	return "", nil
}

// Reload updates the license from the config
func (w *LicenseWatcher) Reload(cfg *LicenseConfig) error {
	blob, err := licenseFromLicenseConfig(cfg)
	if err != nil {
		return err
	}

	_, err = w.SetLicense(blob)
	return err
}

// License atomically returns the license watchers stored license
func (w *LicenseWatcher) License() *nomadLicense.License {
	return w.license.Load().(*nomadLicense.License)
}

// ValidateLicense validates that the given blob is a valid go-licensing
// license as well as a valid nomad license
func (w *LicenseWatcher) ValidateLicense(blob string) (*nomadLicense.License, error) {
	lic, err := w.watcher.ValidateLicense(blob)
	if err != nil {
		return nil, err
	}
	nLic, err := nomadLicense.NewLicense(lic)
	if err != nil {
		return nil, err
	}
	return nLic, nil
}

func (w *LicenseWatcher) SetLicense(blob string) (*licensing.License, error) {
	return w.watcher.SetLicense(blob)
}

func (w *LicenseWatcher) Features() nomadLicense.Features {
	lic := w.license.Load().(*nomadLicense.License)
	if lic == nil {
		return nomadLicense.FeatureNone
	}

	// check if our local license has expired
	if time.Now().After(lic.TerminationTime) {
		return nomadLicense.FeatureNone
	}

	return lic.Features
}

// FeatureCheck determines if the given feature is included in License
// if emitLog is true, a log will only be sent once ever 5 minutes per feature
func (w *LicenseWatcher) FeatureCheck(feature nomadLicense.Features, emitLog bool) error {
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

func (w *LicenseWatcher) hasFeature(feature nomadLicense.Features) bool {
	return w.Features().HasFeature(feature)
}

// start the license watching process in a goroutine. Callers are responsible
// for ensuring it is shut down properly
func (w *LicenseWatcher) start(ctx context.Context) {
	w.once.Do(func() {
		go w.watch(ctx)
		go w.temporaryLicenseMonitor(ctx)
		go w.monitorRaft(ctx)
	})
}

// TODO(drew) rename
func (w *LicenseWatcher) watch(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			w.watcher.Stop()
			return
		// Handle updated license from the watcher
		case lic := <-w.watcher.UpdateCh():
			w.logger.Debug("received update from license manager")

			// Check if watcher has a license and if it needs to be updated
			watcherLicense := w.License()
			if watcherLicense == nil || !watcherLicense.Equal(lic) {
				// Update license
				nomadLicense, err := nomadLicense.NewLicense(lic)
				if err == nil {
					w.license.Store(nomadLicense)
					w.monitorExpTmpCancel()
				} else {
					w.logger.Error("error loading Nomad license", "error", err)
				}
			}

		// Handle licensing watcher errors, primarily expirations.
		case err := <-w.watcher.ErrorCh():
			w.logger.Error("license expired, please update license", "error", err) //TODO: more info

		case warnLicense := <-w.watcher.WarningCh():
			w.logger.Warn("license expiring", "time_left", time.Until(warnLicense.ExpirationTime).Truncate(time.Second))
		}
	}
}

func (w *LicenseWatcher) monitorRaft(ctx context.Context) {
	// TODO(Drew) use config settings for limiter
	limiter := rate.NewLimiter(rate.Limit(1), 1)

	var lastSigned string
	for {
		if err := limiter.Wait(ctx); err != nil {
			return
		}

		update, lic, err := w.waitForLicenseUpdate(ctx, lastSigned)
		if err != nil {
			w.logger.Warn("License sync error (will retry)", "error", err)
			select {
			case <-ctx.Done():
				return
			default:
			}
		}

		if update {
			// check if this license should be set
			// Force == true or license is newer
			nomadLic, err := w.ValidateLicense(lic.Signed)
			if err != nil {
				w.logger.Error("failed to validate license from update", "error", err)
				continue
			}

			// Update this server's license if it is newer or if it should be
			// overridden
			current := w.License()
			if current.Temporary ||
				nomadLic.IssueTime.After(current.IssueTime) ||
				lic.Force {
				_, err := w.SetLicense(lic.Signed)
				if err != nil {
					w.logger.Error("failed to set license from update", "error", err)
				} else {
					lastSigned = lic.Signed
				}
			}
		}
	}
}

func (w *LicenseWatcher) waitForLicenseUpdate(ctx context.Context, lastSigned string) (bool, *structs.StoredLicense, error) {
	state := w.stateStoreFn()
	ws := state.NewWatchSet()
	ws.Add(ctx.Done())

	// Perform initial query, attaching watchset
	lic, err := state.License(ws)
	if err != nil {
		return false, nil, err
	}

	if lic != nil && lic.Signed != lastSigned {
		return true, lic, nil
	}

	// Wait for trigger
	ws.Watch(nil)

	updateLic, err := w.stateStoreFn().License(ws)
	if updateLic != nil && updateLic.Signed != lastSigned {
		return true, updateLic, err
	} else if updateLic == nil && lic != nil {
		return true, nil, err
	} else {
		return false, nil, err
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
			w.logger.Debug("license is not temporary, temporary license monitor exiting")
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
	nomadLicense, err := nomadLicense.NewLicense(lic)
	if err != nil {
		return err
	}

	if !nomadLicense.Temporary && w.propagateFn != nil {
		return w.propagateFn(nomadLicense, signed)
	}

	return nil
}

// tempLicenseTooOld checks if the cluster age is older than the temporary
// license time limit
func tmpLicenseTooOld(originalTmpCreate time.Time) bool {
	return time.Now().After(originalTmpCreate.Add(temporaryLicenseTimeLimit))
}

//

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
