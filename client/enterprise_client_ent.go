// +build ent

package client

import (
	"fmt"
	"sync/atomic"

	"github.com/hashicorp/nomad-licensing/license"
)

// EnterpriseClient is used to keep feature state and check features
type EnterpriseClient struct {
	features uint64
}

func newEnterpriseClient() *EnterpriseClient {
	return &EnterpriseClient{0}
}

// GetFeatures fetches the unint64 and casts it into the appropriate type
func (ec *EnterpriseClient) GetFeatures() license.Features {
	uf := atomic.LoadUint64(&ec.features)
	return license.Features(uf)
}

// FeatureCheck checks whether or not a feature is licensed. Callers must only compare one
// feature at a time and not combine them in the check.
func (ec *EnterpriseClient) FeatureCheck(feature license.Features, emitLog bool) error {
	setFeatures := ec.GetFeatures()
	if !setFeatures.HasFeature(feature) {
		return fmt.Errorf("feature %q is unlicensed", feature)
	}
	return nil
}

// SetFeature atomically sets the unint64 for EnterpriseClient
func (ec *EnterpriseClient) SetFeatures(u uint64) {
	atomic.StoreUint64(&ec.features, u)
}
