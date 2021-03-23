package licensing

import (
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	// ErrLicenseEmpty is returned when trying to set an empty license
	ErrLicenseEmpty = errors.New("empty license")

	// ErrNilManager is returned when methods on Watcher are called when
	// its manager field is nil
	ErrNilManager = errors.New("license manager is nil")

	// ErrProductRequired is returned with a license has no product name
	ErrProductRequired = errors.New("product required in license")

	// ErrInstallationIDRequired is returned when a license has no installation ID
	ErrInstallationIDRequired = errors.New("installation ID required in license")

	// ErrProductNameMismatch is returned when the Watcher product name does
	// not match the license's product name
	ErrProductNameMismatch = errors.New("product name mismatch")
)

// WatcherOptions contains options for configuring the license
// watcher
type WatcherOptions struct {
	// ProductName is the name of the product for the license
	ProductName string

	// InstallationID is the id for the current installation
	InstallationID string

	// InitLicense is the license to initialize the watcher with
	InitLicense string

	// CallbackFunc is called between validating the license and registering it.
	// If the callback returns an error then the license will not be registered.
	CallbackFunc func(*License, string) error

	// AdditionalPublicKeys are used to
	AdditionalPublicKeys []string

	// ExpirationCheckInterval sets the check interval
	ExpirationCheckInterval time.Duration

	// WarningThresholds contains the intervals where license warnings should
	// be emitted
	WarningThresholds WarningThresholds
}

// Watcher is used to manage licensing events
type Watcher struct {
	l sync.RWMutex

	manager *LicenseManager

	productName    string
	installationID string
	callbackFunc   func(*License, string) error

	warningTimer      *time.Timer
	warningThresholds WarningThresholds

	updateCh  chan *License
	warningCh chan *License
	errorCh   chan error

	watchCh  chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewWatcher creates a new license watcher
func NewWatcher(options *WatcherOptions) (*Watcher, *License, error) {
	return newWatcher(options, nil)
}

// NewWatcherErrorCh creates a new license watcher with a provided
// error channel
func NewWatcherErrorCh(options *WatcherOptions, errCh chan error) (*Watcher, *License, error) {
	if errCh == nil {
		return nil, nil, errors.New("provided error chan cannot be nil")
	}
	return newWatcher(options, errCh)
}

func newWatcher(options *WatcherOptions, errCh chan error) (*Watcher, *License, error) {
	if options.ProductName == "" {
		return nil, nil, errors.New("product name required")
	}
	if options.InitLicense == "" {
		return nil, nil, errors.New("init license required")
	}

	licenseManager, err := NewLicenseManager(options.AdditionalPublicKeys)
	if err != nil {
		return nil, nil, err
	}
	if options.ExpirationCheckInterval > 0 {
		licenseManager.SetExpirationCheckInterval(options.ExpirationCheckInterval)
	}

	warningThresholds := options.WarningThresholds
	if len(warningThresholds) == 0 {
		warningThresholds = defaultWarningThresholds()
	}
	// Sort in ascending order
	sort.Sort(warningThresholds)

	if errCh == nil {
		errCh = make(chan error)
	}

	watcher := &Watcher{
		manager:           licenseManager,
		productName:       options.ProductName,
		installationID:    options.InstallationID,
		callbackFunc:      options.CallbackFunc,
		warningThresholds: warningThresholds,
		errorCh:           errCh,
		warningCh:         make(chan *License),
		updateCh:          make(chan *License, 20),
		stopCh:            make(chan struct{}),
	}

	if _, err := watcher.SetLicense(options.InitLicense); err != nil {
		return nil, nil, err
	}
	license, err := watcher.reset()
	if err != nil {
		return nil, nil, err
	}

	go watcher.start()

	return watcher, license, nil
}

// SetInstallationID sets the installation ID for the watcher. There are some
// cases where the installation ID is not known when initializing the watcher
// and we need to set it after initializing.
func (w *Watcher) SetInstallationID(id string) {
	w.l.Lock()
	defer w.l.Unlock()
	w.installationID = id
}

// UpdateCh returns the channel that notifies of any licensing updates.
func (w *Watcher) UpdateCh() <-chan *License {
	return w.updateCh
}

// ErrorCh returns the channel that notifies of any licensing errors. This
// channel will block without a listener.
func (w *Watcher) ErrorCh() <-chan error {
	return w.errorCh
}

// WarningCh returns the channel for listening to licensing warnings
func (w *Watcher) WarningCh() <-chan *License {
	return w.warningCh
}

func (w *Watcher) start() (retErr error) {
	w.l.RLock()
	currWarningTimer := w.warningTimer
	w.l.RUnlock()

	licenseErrorCh := make(chan error, 1)
	for {
		select {
		case <-w.watchCh:
			license, err := w.reset()
			if err != nil {
				licenseErrorCh <- err
				continue
			}

			// Send the license to the update channel
			select {
			case w.updateCh <- license:
			default:
			}

		case <-currWarningTimer.C:
			license, err := w.License()
			if err != nil {
				licenseErrorCh <- err
				continue
			}
			// the license should only be nil while in the process of
			// shutting down
			if license == nil {
				continue
			}

			// Send the license to the warning channel
			select {
			case w.warningCh <- license:
			default:
			}

			// Reset the timer
			w.resetWarningTimer(license.ExpirationTime)

		case err := <-licenseErrorCh:
			// Teardown the license manager
			w.l.Lock()
			w.manager.Stop()

			// Stop the warning timer
			w.warningTimer.Stop()
			w.l.Unlock()

			// If nothing is listening on the error channel, panic
			select {
			case w.errorCh <- err:
			default:
				panic(err)
			}

		case <-w.stopCh:
			w.manager.Stop()
			return nil
		}
	}
}

// Stop stops the license manager and should be called on when stopping
// its caller to avoid leaking goroutines.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
	})
}

// License returns the currently installed license
func (w *Watcher) License() (*License, error) {
	w.l.RLock()
	defer w.l.RUnlock()

	if w.manager == nil {
		return nil, errors.New("license manager is nil")
	}

	return w.manager.License()
}

// ValidateLicense parses and checks that the license signature is valid and performs
// additional checks without inserting the license
func (w *Watcher) ValidateLicense(licenseStr string) (*License, error) {
	w.l.RLock()
	defer w.l.RUnlock()
	if w.manager == nil {
		return nil, ErrNilManager
	}

	// If there is no license, move on with existing license. We still pass it
	// in to verify the license manager is still running.
	if licenseStr == "" {
		return nil, ErrLicenseEmpty
	}

	// Validate the license signature to get a parsed license back
	// to validate license fields
	license, err := w.manager.Validate(licenseStr)
	if err != nil {
		return nil, err
	}

	// Product is required on the license
	if license.Product == "" {
		return nil, ErrProductRequired
	}

	// InstallationID is required
	if license.InstallationID == "" {
		return nil, ErrInstallationIDRequired
	}

	// Verify product name
	if license.Product != "*" && license.Product != w.productName {
		return nil, ErrProductNameMismatch
	}

	// Verify that the installation ID provided in the license matches the
	// installation ID.  If the InstallationID in the license equals *,
	// ignore the installation ID.
	if license.InstallationID != "*" && license.InstallationID != w.installationID {
		return nil, errors.New("installation id mismatch")
	}

	// If provided, run the validation function
	if w.callbackFunc != nil {
		if err := w.callbackFunc(license, licenseStr); err != nil {
			return nil, err
		}
	}

	return license, nil
}

// SetLicense registers the license with the manager if the license manager
// is available.  The parsed license is returned if all validation passes.
func (w *Watcher) SetLicense(licenseStr string) (*License, error) {

	// Perform the signature check and additional validations
	license, err := w.ValidateLicense(licenseStr)
	if err != nil {
		return nil, err
	}

	// Actually store the license in the manager
	if err := w.manager.registerLicense(license); err != nil {
		return nil, err
	}
	return license, nil
}

func (w *Watcher) reset() (*License, error) {
	w.watchCh = make(chan struct{})
	w.l.RLock()
	license, err := w.manager.RegisterWatcher(w.watchCh)
	w.l.RUnlock()
	if err != nil {
		return nil, err
	}
	if license == nil {
		return nil, errors.New("invalid license or license is expired")
	}

	w.resetWarningTimer(license.ExpirationTime)
	return license, nil
}

// IsWarning returns if the current expiration time is in a warning state and
// the associated time left on the license
func (w *Watcher) IsWarning(expirationTime time.Time) (bool, time.Duration) {
	var isWarning bool
	timeLeft := time.Until(expirationTime)
	for _, threshold := range w.warningThresholds {
		if timeLeft < threshold.TimeLeft {
			isWarning = true
			break
		}
	}
	return isWarning, timeLeft
}

func (w *Watcher) resetWarningTimer(expirationTime time.Time) {
	var nextWarning time.Duration

	timeLeft := time.Until(expirationTime)
	for _, threshold := range w.warningThresholds {
		if timeLeft < threshold.TimeLeft {
			nextWarning = threshold.NextWarning
			break
		}
	}

	// By default set to the longest threshold
	if nextWarning == 0 {
		nextWarning = timeLeft - w.warningThresholds[len(w.warningThresholds)-1].TimeLeft
	}

	w.l.Lock()
	defer w.l.Unlock()
	if w.warningTimer == nil {
		w.warningTimer = time.NewTimer(nextWarning)
		return
	}
	w.warningTimer.Reset(nextWarning)
}

func defaultWarningThresholds() WarningThresholds {
	return WarningThresholds{
		WarningTimer{
			TimeLeft:    time.Hour,
			NextWarning: 1 * time.Minute,
		},
		WarningTimer{
			TimeLeft:    24 * time.Hour,
			NextWarning: 5 * time.Minute,
		},
		WarningTimer{
			TimeLeft:    7 * 24 * time.Hour,
			NextWarning: 1 * time.Hour,
		},
		WarningTimer{
			TimeLeft:    30 * 24 * time.Hour,
			NextWarning: 24 * time.Hour,
		},
	}
}

// WarningTimer displays the next warning time with a certain amount of time
// left
type WarningTimer struct {
	TimeLeft    time.Duration
	NextWarning time.Duration
}

// WarningThresholds contains all the warning timer thresholds
type WarningThresholds []WarningTimer

func (s WarningThresholds) Len() int {
	return len(s)
}
func (s WarningThresholds) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s WarningThresholds) Less(i, j int) bool {
	return s[i].TimeLeft < s[j].TimeLeft
}
