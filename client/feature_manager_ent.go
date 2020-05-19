package client

import (
	"sync/atomic"
)

type featureManager struct {
	features uint64
}

func (fm *featureManager) GetFeatures() uint64 {
	return atomic.LoadUint64(&fm.features)
}

func (fm *featureManager) HasFeature(u uint64) bool {
	return (atomic.LoadUint64(&fm.features) & u) != 0
}

func (fm *featureManager) SetFeature(u uint64) {
	atomic.StoreUint64(&fm.features, u)
	return
}
