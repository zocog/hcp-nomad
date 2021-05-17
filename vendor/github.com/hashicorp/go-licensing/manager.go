package licensing

import (
	"crypto"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/errwrap"
	multierror "github.com/hashicorp/go-multierror"
)

var (
	// The default expiration check interval
	defaultExpirationCheckInterval = 30 * time.Second

	ErrInvalidIntermediatePublicKey     = errors.New("invalid intermediate public key")
	ErrInvalidLicenseViaIntermediateSig = errors.New("license bytes not verified by intermediate signature")
)

// LicenseManager manages license lifecycle and validation
type LicenseManager struct {
	l sync.RWMutex

	// The public keys valid for signature verification
	pubKeys []crypto.PublicKey

	// Stores channels to notify on license change; this includes expiration
	notificationChannels []chan struct{}

	// Controls when we check for expiration
	expirationTicker        *time.Ticker
	expirationCheckInterval time.Duration

	// Tells the expiration checker to stop
	stopCh chan struct{}

	// The current license information
	license *License
}

// NewLicenseManager creates a new license manager that uses the given public
// key for verification. The pubic key must be a base64-encoded byte slice.
func NewLicenseManager(supplementalPubKeys []string) (*LicenseManager, error) {
	ret := &LicenseManager{
		expirationCheckInterval: defaultExpirationCheckInterval,
	}

	allKeys := append(builtInPubKeys, supplementalPubKeys...)
	for _, pubKey := range allKeys {
		keyBytes, err := base64.StdEncoding.DecodeString(pubKey)
		if err != nil {
			return nil, errwrap.Wrapf("error decoding public key: {{err}}", err)
		}

		if len(keyBytes) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("public key is the wrong size")
		}

		ret.pubKeys = append(ret.pubKeys, ed25519.PublicKey(keyBytes))
	}

	return ret, nil
}

// Stop tears down the manager; in practice this mostly stops the ticker that
// watches for expiration
func (l *LicenseManager) Stop() {
	l.l.Lock()
	defer l.l.Unlock()
	l.teardown()
}

// SetExpirationCheckInterval sets how often the license manager ticker
// will check the license for exipiration.  This will only affect new license
// registrations.
func (l *LicenseManager) SetExpirationCheckInterval(interval time.Duration) {
	l.l.Lock()
	defer l.l.Unlock()
	l.expirationCheckInterval = interval
}

// Validate checks whether the license is valid by any known public
// key and within its allowed timing. It returns an error if one was
// encountered, or the license and a nil error if the license is valid.
func (l *LicenseManager) Validate(license string) (*License, error) {
	licenseBytes, err := l.validateSignature(license)
	if err != nil {
		return nil, err
	}

	return l.validateLicense(licenseBytes)
}

// validateBytes is used to validate arbitrary bytes slices against
// known public keys with the provided signature.
func (l *LicenseManager) validateBytes(blob, sigBytes []byte) error {
	for _, pubKey := range l.pubKeys {
		switch pubkey := pubKey.(type) {
		case ed25519.PublicKey:
			if ed25519.Verify(pubkey, blob, sigBytes) {
				return nil
			}
		default:
			return fmt.Errorf("unsupported key type")
		}
	}
	return fmt.Errorf("signature invalid for license; tried %d key(s)", len(l.pubKeys))
}

// Internal method to parse and validate a license. If the license is valid,
// the validated bytes comprising the information JSON is returned and the
// error is nil, otherwise the error describes the problem.
func (l *LicenseManager) validateSignature(license string) ([]byte, error) {
	if len(license) == 0 {
		return nil, fmt.Errorf("empty license data")
	}

	if len(l.pubKeys) == 0 {
		return nil, fmt.Errorf("no public keys")
	}

	ver, license, err := ParseVersion(license)
	if err != nil {
		return nil, err
	}

	// Validate that we have a known version and react accordingly
	switch ver {
	case 1:
		licenseBytes, sigStr, err := V1Parse(license)
		if err != nil {
			return nil, err
		}
		sig, err := v1SigParse(sigStr)
		if err != nil {
			return nil, err
		}
		if err = l.validateBytes(licenseBytes, sig); err != nil {
			return nil, err
		}
		return licenseBytes, nil
	case 2:
		licenseBytes, sigStr, err := V1Parse(license)
		if err != nil {
			return nil, err
		}
		intSig, ia, err := v2SigParse(sigStr)
		if err != nil {
			return nil, err
		}
		if err = l.validateV2License(ia, licenseBytes, intSig); err != nil {
			return nil, err
		}
		return licenseBytes, nil
	default:
		return nil, fmt.Errorf("unknown version: %d", ver)
	}
}

// validateV2License first checks the validity of the intermediate public
// key with a stored public key and then checks the license payload with the
// intermediate public key
func (l *LicenseManager) validateV2License(ia *IntermediateAuthority, licenseBytes, intSig []byte) error {
	// First validate the intermediate public key
	if err := l.validateBytes([]byte(ia.PublicKey), ia.RootSignature); err != nil {
		return ErrInvalidIntermediatePublicKey
	}
	// Then use the intermediate public key to validate the license payload
	if !ed25519.Verify(ia.PublicKey, licenseBytes, intSig) {
		return ErrInvalidLicenseViaIntermediateSig
	}
	return nil
}

// RegisterLicense takes in a license string, validates it, and parses
// information out. It notifies any watchers.
func (l *LicenseManager) RegisterLicense(license string) error {
	lic, err := l.Validate(license)
	if err != nil {
		return err
	}
	return l.registerLicense(lic)
}

func (l *LicenseManager) registerLicense(license *License) error {
	// Make a copy of the license
	license, err := license.Clone()
	if err != nil {
		return err
	}

	l.l.Lock()
	defer l.l.Unlock()

	// reset state
	l.teardown()

	l.license = license
	l.expirationTicker = time.NewTicker(l.expirationCheckInterval)
	l.stopCh = make(chan struct{})

	go l.watchTiming(l.expirationTicker, l.stopCh)

	l.notifyWatchers()

	return nil
}

// License returns a License object containing the current license
// information. If no license has been read, the object will be nil.  It is
// safe to call this concurrently.
func (l *LicenseManager) License() (*License, error) {
	l.l.RLock()
	defer l.l.RUnlock()

	return l.license.Clone()
}

// RegisterWatcher allows a client to provide a channel to be notified of any
// changes in the license. The current license is also returned, so that the
// caller knows they will get any changes after that version. The given channel
// will be closed when a license change occurs. It is safe to call this
// concurrently.
func (l *LicenseManager) RegisterWatcher(ch chan struct{}) (*License, error) {
	if ch == nil {
		return nil, fmt.Errorf("given notification channel is nil")
	}

	l.l.Lock()
	defer l.l.Unlock()

	l.notificationChannels = append(l.notificationChannels, ch)
	return l.license.Clone()
}

// Performs common teardown logic. The lock must be held prior to calling this.
func (l *LicenseManager) teardown() {
	l.license = nil
	if l.expirationTicker != nil {
		l.expirationTicker.Stop()
		l.expirationTicker = nil
	}
	if l.stopCh != nil {
		close(l.stopCh)
		l.stopCh = nil
	}
}

func (l *LicenseManager) notifyWatchers() {
	for _, ch := range l.notificationChannels {
		close(ch)
	}

	l.notificationChannels = l.notificationChannels[:0]
}

// watchTiming watches for expiration of the license
func (l *LicenseManager) watchTiming(expiration *time.Ticker, stopCh chan struct{}) {

	for {
		select {
		case <-expiration.C:
			now := time.Now()
			l.l.Lock()

			// This happens because we've torn down but we hit the expiration
			// ticker before the stop channel, so return
			if l.license == nil {
				l.l.Unlock()
				return
			}

			// If we are before or after the valid license period,
			// immediately clear licensing information
			if now.Before(l.license.StartTime) || now.After(l.license.TerminationTime) {
				l.teardown()
				l.notifyWatchers()
			}
			l.l.Unlock()

		case <-stopCh:
			return
		}
	}
}

// Internal method to parse license bytes and perform any needed validations
func (l *LicenseManager) validateLicense(licenseBytes []byte) (*License, error) {
	lic := new(License)
	if err := json.Unmarshal(licenseBytes, lic); err != nil {
		return nil, errwrap.Wrapf("error parsing JSON license: {{err}}", err)
	}

	// If not set, default the value of termination time to expiration time. This behavior
	// is required for backwards compatibility of licenses issued before termination time
	// was included in the license
	if lic.TerminationTime.IsZero() {
		lic.TerminationTime = lic.ExpirationTime
	}

	now := time.Now()

	var err error
	if lic.StartTime.IsZero() {
		err = multierror.Append(err, fmt.Errorf("license is missing a start time"))
	}
	if lic.StartTime.After(now) {
		err = multierror.Append(err, fmt.Errorf("license is not yet valid"))
	}
	if lic.ExpirationTime.IsZero() {
		err = multierror.Append(err, fmt.Errorf("license is missing an expiration time"))
	}
	if lic.TerminationTime.Before(lic.ExpirationTime) {
		err = multierror.Append(err, fmt.Errorf("license termination time is before expiration time"))
	}
	if lic.TerminationTime.Before(now) {
		err = multierror.Append(err, fmt.Errorf("license is no longer valid"))
	}
	if lic.IssueTime.IsZero() {
		err = multierror.Append(err, fmt.Errorf("license is missing an issue time"))
	}
	if lic.IssueTime.After(now) {
		err = multierror.Append(err, fmt.Errorf("issue time is in the future"))
	}
	if lic.Product == "" {
		err = multierror.Append(err, fmt.Errorf("license is missing a product"))
	}
	if lic.CustomerID == "" {
		err = multierror.Append(err, fmt.Errorf("license is missing a customer ID"))
	}
	if lic.LicenseID == "" {
		err = multierror.Append(err, fmt.Errorf("license is missing a license ID"))
	}

	if err != nil {
		return nil, err
	}
	return lic, nil
}
