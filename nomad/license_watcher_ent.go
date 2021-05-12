// +build ent

package nomad

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-licensing"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
)

var (
	ErrOlderLicense = fmt.Errorf("requested license is older than current one, use force to override")
)

const (
	// permanentLicenseID is the license ID used for permanent (s3) enterprise builds
	permanentLicenseID = "permanent"

	licenseExpired = "license is no longer valid"
)

// LicenseConfig allows for tunable licensing config
// primarily used for enterprise testing
type LicenseConfig struct {
	// LicenseEnvBytes is the license bytes to use for the server's license
	LicenseEnvBytes string

	// LicensePath is the path to use for the server's license
	LicensePath string

	// AdditionalPubKeys is a set of public keys to
	AdditionalPubKeys []string

	Logger hclog.InterceptLogger
}

// ServerLicense contains an expanded license and its corresponding blob
type ServerLicense struct {
	license *nomadLicense.License
	blob    string
}

type LicenseWatcher struct {
	// TODO: it might be possible to avoid this atomic now that we've removed
	// raft updates; the whole configuration needs to be updated
	// license is the watchers atomically stored ServerLicense
	licenseInfo atomic.Value

	// fileLicense is the license loaded from the server's license path or env
	// it is set when the LicenseWatcher is initialized and when Reloaded.
	fileLicense string

	watcher *licensing.Watcher

	logMu  sync.Mutex
	logger hclog.Logger

	// logTimes tracks the last time a log message was sent for a feature
	logTimes map[nomadLicense.Features]time.Time
}

func NewLicenseWatcher(cfg *LicenseConfig) (*LicenseWatcher, error) {
	blob, err := licenseFromLicenseConfig(cfg)
	if err != nil {
		return nil, err
	}
	if blob == "" {
		return nil, errors.New("failed to read license: license is missing. To add a license, configure \"license_path\" in your server configuration file, use the NOMAD_LICENSE environment variable, or use the NOMAD_LICENSE_PATH environment variable. For a trial license of Nomad Enterprise, visit https://nomadproject.io/trial.")
	}

	lw := &LicenseWatcher{
		fileLicense: blob,
		logger:      cfg.Logger.Named("licensing"),
		logTimes:    make(map[nomadLicense.Features]time.Time),
	}

	opts := &licensing.WatcherOptions{
		ProductName:          nomadLicense.ProductName,
		InitLicense:          blob,
		AdditionalPublicKeys: cfg.AdditionalPubKeys,
	}

	// Create the new watcher with options
	watcher, _, err := licensing.NewWatcher(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize nomad license: %w", err)
	}
	lw.watcher = watcher

	err = lw.SetLicense(blob)
	if err != nil {
		return nil, fmt.Errorf("failed to set nomad license: %w", err)
	}
	return lw, nil
}

// Reload updates the license from the config
func (lw *LicenseWatcher) Reload(cfg *LicenseConfig) error {
	blob, err := licenseFromLicenseConfig(cfg)
	if err != nil {
		return err
	}
	if blob == "" {
		return nil
	}

	return lw.SetLicense(blob)
}

// License atomically returns the license watchers stored license
func (w *LicenseWatcher) License() *nomadLicense.License {
	return w.licenseInfo.Load().(*ServerLicense).license
}

func (w *LicenseWatcher) LicenseBlob() string {
	return w.licenseInfo.Load().(*ServerLicense).blob
}

// FileLicense returns the watchers file license that was used to initialize
// the server. It is not necessarily the license that the server is currently using
// if a newer license was added via raft or manual API invocation
func (w *LicenseWatcher) FileLicense() string {
	return w.fileLicense
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

func (w *LicenseWatcher) Features() nomadLicense.Features {
	lic := w.License()
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
func (lw *LicenseWatcher) FeatureCheck(feature nomadLicense.Features, emitLog bool) error {
	if lw.hasFeature(feature) {
		return nil
	}

	err := fmt.Errorf("Feature %q is unlicensed", feature.String())

	if emitLog {
		// Only send log messages for a missing feature every 5 minutes
		lw.logMu.Lock()
		defer lw.logMu.Unlock()
		lastTime := lw.logTimes[feature]
		now := time.Now()
		if now.Sub(lastTime) > 5*time.Minute {
			lw.logger.Warn(err.Error())
			lw.logTimes[feature] = now
		}
	}

	return err
}

// SetLicense sets the server's license
func (lw *LicenseWatcher) SetLicense(blob string) error {
	blob = strings.TrimRight(blob, "\r\n")

	_, err := lw.watcher.ValidateLicense(blob)
	if err != nil {
		return fmt.Errorf("error validating license: %w", err)
	}

	if _, err := lw.watcher.SetLicense(blob); err != nil {
		lw.logger.Error("failed to persist license", "error", err)
		return err
	}

	startUpLicense, err := lw.watcher.License()
	if err != nil {
		return fmt.Errorf("failed to retrieve license: %w", err)
	}

	license, err := nomadLicense.NewLicense(startUpLicense)
	if err != nil {
		return fmt.Errorf("failed to convert license: %w", err)
	}

	// Store the expanded license and the corresponding blob
	lw.licenseInfo.Store(&ServerLicense{
		license: license,
		blob:    blob,
	})

	return nil
}

func (lw *LicenseWatcher) hasFeature(feature nomadLicense.Features) bool {
	return lw.Features().HasFeature(feature)
}

// start the license watching process in a goroutine. Callers are responsible
// for ensuring it is shut down properly
func (lw *LicenseWatcher) start(ctx context.Context) {
	go lw.monitorWatcher(ctx)
}

// monitorWatcher monitors the LicenseWatchers go-licensing watcher channels
//
// Nomad uses the go licensing watcher channels mostly to log.  Since Nomad
// does not shut down when a valid license has expired the ErrorCh logs.
func (lw *LicenseWatcher) monitorWatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			lw.watcher.Stop()
			return
		// Handle updated license from the watcher
		case <-lw.watcher.UpdateCh():
			// Check if server is shutting down
			select {
			case <-ctx.Done():
				return
			default:
			}
			lw.logger.Debug("received update from license manager")

		// Handle licensing watcher errors, primarily expirations.
		case err := <-lw.watcher.ErrorCh():
			//TODO: more info
			lw.logger.Error("license expired, please update license", "error", err)

		case warnLicense := <-lw.watcher.WarningCh():
			lw.logger.Warn("license expiring", "time_left", time.Until(warnLicense.ExpirationTime).Truncate(time.Second))
		}
	}
}

func licenseFromLicenseConfig(cfg *LicenseConfig) (string, error) {
	if cfg.LicenseEnvBytes != "" {
		return cfg.LicenseEnvBytes, nil
	}

	if cfg.LicensePath != "" {
		licRaw, err := ioutil.ReadFile(cfg.LicensePath)
		if err != nil {
			return "", fmt.Errorf("failed to read license file %w", err)
		}
		return strings.TrimRight(string(licRaw), "\r\n"), nil
	}

	blob, err := defaultEnterpriseLicense(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to set default license %w", err)
	}
	return blob, nil
}
