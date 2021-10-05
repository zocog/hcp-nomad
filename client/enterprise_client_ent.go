//go:build ent
// +build ent

package client

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-licensing/license"
)

// EnterpriseClient is used to keep feature state and check features
type EnterpriseClient struct {
	// features should be accessed atomically
	features uint64

	logMu sync.Mutex
	// logTimes tracks the last time we sent a log message for a feature
	logTimes map[license.Features]time.Time

	logger hclog.Logger
}

func newEnterpriseClient(logger hclog.Logger) *EnterpriseClient {
	return &EnterpriseClient{
		features: uint64(license.AllFeatures()),
		logMu:    sync.Mutex{},
		logTimes: make(map[license.Features]time.Time),
		logger:   logger,
	}
}

// GetFeatures fetches the unint64 and casts it into the appropriate type
func (ec *EnterpriseClient) GetFeatures() license.Features {
	return license.Features(atomic.LoadUint64(&ec.features))
}

// FeatureCheck checks whether or not a feature is licensed. Callers must only compare one
// feature at a time and not combine them in the check.
func (ec *EnterpriseClient) FeatureCheck(feature license.Features, emitLog bool) error {
	if !ec.GetFeatures().HasFeature(feature) {
		err := fmt.Errorf("feature %q is unlicensed", feature)
		if emitLog {
			ec.logMu.Lock()
			defer ec.logMu.Unlock()
			// Only send log messages for a missing feature every 5 minutes
			lastTime := ec.logTimes[feature]
			now := time.Now()
			if now.Sub(lastTime) > 5*time.Minute {
				ec.logger.Warn(err.Error())
				ec.logTimes[feature] = now
			}
		}
		return err
	}
	return nil
}

// SetFeature atomically sets the unint64 for EnterpriseClient
func (ec *EnterpriseClient) SetFeatures(u uint64) {
	atomic.StoreUint64(&ec.features, u)
}
