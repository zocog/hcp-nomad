// +build consulent

package license

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/hashicorp/consul/logging"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-licensing"
	"golang.org/x/crypto/ed25519"
)

const (
	// product information
	productName = "consul"
)

var (
	builtinPublicKeys = []string{
		"mP7CYlXOqUs641EuzMQb2xrnxuaHCZWS0aiTh087T+U=", // Production Key"
	}
)

type licenseLog struct {
	level       string
	message     string
	quietPeriod time.Duration
}

type LicenseManager struct {
	// Initial license, potentially being used for later reset
	initialLicenseString string

	// Expanded/parsed License
	license *License

	// The signed license blob
	signed atomic.Value

	// The current feature set
	features uint32

	// The previous set of features prior to a license changing/expiring
	previousFeatures uint32

	// callback to issue when the license has terminated and Consul should be shut down
	errorFn func(*License, string)

	// callback to use after validation but prior to setting the license
	// this can short circuit SetLicense and cause it to error out setting
	// a valid license
	propagateFn func(*License, string) error

	// internal log watcher that will monitor the expiration of the license
	watcher *licensing.Watcher

	// Channel used for retrieving the current license in a safe way
	licenseCh chan *License

	logger hclog.Logger

	// channel for synchronizing license manager logging including checking
	logCh chan *licenseLog

	// used for when we need to recreate the watcher
	expirationCheckInterval time.Duration
}

type LicensingConfig struct {
	AdditionalPublicKeys    ed25519.PublicKey
	ExpirationCheckInterval time.Duration
	TemporaryLicensePackage string
	IsServer                bool
	ErrorFn                 func(*License, string)
	PropagateFn             func(*License, string) error
}

func tempLicense(pkg string, duration time.Duration) (ed25519.PublicKey, string, error) {
	if pkg == "" {
		pkg = temporaryLicensePackage
	}
	now := time.Now()
	lic := &licensing.License{
		LicenseID:      temporaryLicenseID,
		CustomerID:     temporaryLicenseID,
		IssueTime:      now,
		StartTime:      now.Add(temporaryLicenseStartOffset),
		ExpirationTime: now.Add(duration),
		Product:        productName,
		InstallationID: "*",
		Flags: map[string]interface{}{
			"package":   pkg,
			"temporary": true,
		},
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}

	signed, err := lic.SignedString(privateKey)
	if err != nil {
		return nil, "", err
	}

	return publicKey, signed, nil
}

func NewLicenseManager(logger hclog.Logger, conf *LicensingConfig) (*LicenseManager, error) {
	duration := temporaryClientLicenseDuration
	if conf.IsServer {
		duration = temporaryServerLicenseDuration
	}
	publicKey, signedTemp, err := tempLicense(conf.TemporaryLicensePackage, duration)

	manager := &LicenseManager{
		logger:                  logger.Named(logging.License),
		initialLicenseString:    signedTemp,
		features:                0,
		previousFeatures:        0,
		errorFn:                 conf.ErrorFn,
		propagateFn:             conf.PropagateFn,
		licenseCh:               make(chan *License),
		logCh:                   make(chan *licenseLog),
		expirationCheckInterval: conf.ExpirationCheckInterval,
	}

	manager.signed.Store("")

	keys := []ed25519.PublicKey{publicKey}
	if len(conf.AdditionalPublicKeys) > 0 {
		keys = append(keys, conf.AdditionalPublicKeys)
	}

	err = manager.setupWatcher(signedTemp, keys)
	if err != nil {
		return nil, err
	}

	return manager, nil
}

func (mgr *LicenseManager) Start(cancelCh <-chan struct{}) {
	logTimes := make(map[string]time.Time)
	for {
		select {
		// send the license to whoever asked for it
		case mgr.licenseCh <- mgr.license:
		// detect updates and override the license
		case lic := <-mgr.watcher.UpdateCh():
			if mgr.license == nil || !mgr.license.Equal(lic) {
				consulLicense, err := NewLicense(lic)
				if err == nil {
					prev := Features(mgr.features)
					if mgr.license == nil || !mgr.license.Temporary {
						// only add previous features for non temp licenses
						prev.AddFeature(Features(mgr.previousFeatures))
					}

					mgr.license = consulLicense

					// still need atomics for the writes though
					atomic.StoreUint32(&mgr.previousFeatures, uint32(prev))
					atomic.StoreUint32(&mgr.features, uint32(consulLicense.Features))
					mgr.logger.Info("Consul license updated")
				} else {
					// this really shouldn't happen as we will already have validated that the license was
					// a consul license in the watcher callback func
					mgr.logger.Error("error loading Consul license", "error", err)
				}
			}

		case err := <-mgr.watcher.ErrorCh():
			// This only happens after the hard termination time has been reached
			mgr.logger.Error("licensing error ", "error", err.Error())

			// blow away our current features
			atomic.StoreUint32(&mgr.features, 0)

			if mgr.errorFn != nil {
				if mgr.license.Temporary {
					mgr.errorFn(nil, "")
				} else {
					mgr.errorFn(mgr.license, mgr.signed.Load().(string))
				}
			}

			mgr.signed.Store("")
			mgr.license = nil

			// no more license management - we are going to shutdown
			return
		case license := <-mgr.watcher.WarningCh():
			// the license is emitted over the warning chan when its close to or has expired.
			if license.ExpirationTime.After(time.Now()) {
				mgr.logger.Warn("license close to expiration",
					"expiration_time", license.ExpirationTime.Round(time.Second),
					"time_left", license.ExpirationTime.Sub(time.Now()).Round(time.Second).String(),
				)
			} else {
				mgr.logger.Error("license expired", "expiration_time", license.ExpirationTime.Round(time.Second))
			}

		case logmsg := <-mgr.logCh:
			mgr.logMessage(logTimes, logmsg)
		case <-cancelCh:
			mgr.watcher.Stop()
			return
		}
	}
}

func (mgr *LicenseManager) License() (*License, error) {
	lic := <-mgr.licenseCh
	licenseCopy, err := lic.Clone()
	if err != nil {
		return nil, err
	}
	return licenseCopy, nil
}

func (mgr *LicenseManager) IsWarning(expirationTime time.Time) (bool, time.Duration) {
	return mgr.watcher.IsWarning(expirationTime)
}

// ResetLicense will reset the license to the builtin one if it is still valid.
// If the builtin license is invalid, the current license stays active.
func (mgr *LicenseManager) ResetLicense() (*License, error) {
	return mgr.SetLicense(mgr.initialLicenseString)
}

func (mgr *LicenseManager) SetLicense(signed string) (*License, error) {
	// validate and set the license
	lic, err := mgr.watcher.SetLicense(signed)

	if err != nil {
		return nil, err
	}

	// wrap the go-licensing license with our own
	consulLicense, err := NewLicense(lic)
	if err != nil {
		return nil, err
	}
	return consulLicense, nil
}

func (mgr *LicenseManager) Features() Features {
	return Features(atomic.LoadUint32(&mgr.features))
}

func (mgr *LicenseManager) PreviousFeatures() Features {
	return Features(atomic.LoadUint32(&mgr.previousFeatures))
}

func (mgr *LicenseManager) HasFeature(feature Features) bool {
	return mgr.Features().HasFeature(feature)
}

func (mgr *LicenseManager) HadFeature(feature Features) bool {
	return mgr.PreviousFeatures().HasFeature(feature)
}

func (mgr *LicenseManager) FeatureCheck(feature Features, allowPrevious bool, emitLog bool) error {
	if mgr.HasFeature(feature) {
		return nil
	}

	err := fmt.Errorf("Feature \"%s\" is unlicensed", feature.String())

	if emitLog {
		mgr.logCh <- &licenseLog{message: err.Error(), level: "WARN", quietPeriod: time.Minute * 5}
	}

	if allowPrevious && mgr.HadFeature(feature) {
		return nil
	}

	return err
}

func (mgr *LicenseManager) setupWatcher(initLicense string, additionalPublicKeys []ed25519.PublicKey) error {
	watcherOptions := &licensing.WatcherOptions{
		ProductName:             productName,
		InitLicense:             initLicense,
		CallbackFunc:            mgr.watcherCallback,
		AdditionalPublicKeys:    builtinPublicKeys,
		ExpirationCheckInterval: mgr.expirationCheckInterval,
	}

	// Add public keys
	for _, key := range additionalPublicKeys {
		watcherOptions.AdditionalPublicKeys = append(watcherOptions.AdditionalPublicKeys, base64.StdEncoding.EncodeToString(key))
	}

	watcher, tempLicense, err := licensing.NewWatcher(watcherOptions)
	if err != nil {
		return err
	}
	mgr.watcher = watcher

	if mgr.license, err = NewLicense(tempLicense); err != nil {
		return err
	}

	mgr.signed.Store(initLicense)
	mgr.features = uint32(mgr.license.Features)

	return nil
}

func (mgr *LicenseManager) watcherCallback(lic *licensing.License, signed string) error {
	consulLicense, err := NewLicense(lic)
	if err != nil {
		return err
	}

	var ret error = nil
	if !consulLicense.Temporary && mgr.propagateFn != nil {
		ret = mgr.propagateFn(consulLicense, signed)
	}

	if ret != nil {
		mgr.signed.Store(signed)
	}

	return ret
}

func (mgr *LicenseManager) logMessage(logTimes map[string]time.Time, logmsg *licenseLog) {
	lastTime := logTimes[logmsg.message]
	now := time.Now()
	if now.Sub(lastTime) > logmsg.quietPeriod {
		switch logging.LevelFromString(logmsg.level) {
		case hclog.Trace:
			mgr.logger.Trace(logmsg.message)
		case hclog.Debug:
			mgr.logger.Debug(logmsg.message)
		case hclog.Info:
			mgr.logger.Info(logmsg.message)
		case hclog.Warn:
			mgr.logger.Warn(logmsg.message)
		case hclog.Error:
			mgr.logger.Error(logmsg.message)
		default:
			mgr.logger.Info(logmsg.message)
		}

		logTimes[logmsg.message] = now
	}
}
