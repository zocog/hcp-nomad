package license

import (
	"encoding/json"
	"errors"
	"strings"
)

// Features is a bitmask of feature flags
type Features uint64

// FeatureNone has no features
const FeatureNone Features = 0

const (
	// autopilot nomad upgrades
	FeatureAutoUpgrades Features = 1 << iota

	// non-voting servers
	FeatureReadScalability

	// autopilot redundancy zones
	FeatureRedundancyZones

	// name spaces
	FeatureNamespaces

	// resource quotas
	FeatureResourceQuotas

	// audit logging
	FeatureAuditLogging

	// sentinel policies
	FeatureSentinelPolicies

	// multi-region deployments
	FeatureMultiregionDeployments

	// Should always be last, add features above.
	numOfFeatures uint = iota
)

func (f Features) String() string {
	switch f {
	case FeatureAutoUpgrades:
		return "Automated Upgrades"
	case FeatureReadScalability:
		return "Enhanced Read Scalability"
	case FeatureRedundancyZones:
		return "Redundancy Zones"
	case FeatureNamespaces:
		return "Namespaces"
	case FeatureResourceQuotas:
		return "Resource Quotas"
	case FeatureAuditLogging:
		return "Audit Logging"
	case FeatureSentinelPolicies:
		return "Sentinel Policies"
	case FeatureMultiregionDeployments:
		return "Multiregion Deployments"
	default:
		return "Unknown"
	}
}

// FeatureFromString returns the feature flag for the given string representation
func FeatureFromString(feature string) (Features, error) {
	for _, f := range allFeatures().List() {
		if sanitize(f.String()) == sanitize(feature) {
			return f, nil
		}
	}
	return FeatureNone, errors.New("unknown feature")
}

func sanitize(feature string) string {
	feature = strings.ToLower(feature)
	return strings.Replace(feature, " ", "", -1)
}

func AllFeatures() Features {
	return allFeatures()
}

func allFeatures() Features {
	return Features((1 << numOfFeatures) - 1)
}

// HasFeature checks the features flags for the given flag
func (f Features) HasFeature(flag Features) bool {
	return f&flag != 0
}

// AddFeature adds a feature
func (f *Features) AddFeature(flag Features) {
	*f |= flag
}

// RemoveFeature removes a feature
func (f *Features) RemoveFeature(flag Features) {
	*f &= ^flag
}

// List returns all features installed
func (f Features) List() []Features {
	var features []Features
	var i uint = 0
	for ; i < numOfFeatures; i++ {
		feature := Features(1 << uint(i))
		if f.HasFeature(feature) {
			features = append(features, feature)
		}
	}
	return features
}

func (f Features) StringList() []string {
	var features []string
	var i uint = 0
	for ; i < numOfFeatures; i++ {
		feature := Features(1 << uint(i))
		if f.HasFeature(feature) {
			features = append(features, feature.String())
		}
	}

	return features
}

func (f *Features) MarshalJSON() ([]byte, error) {
	flist := f.List()
	farray := make([]string, 0, len(flist))
	for _, f := range flist {
		farray = append(farray, f.String())
	}
	return json.Marshal(farray)
}

func (f *Features) UnmarshalJSON(b []byte) error {
	farray := make([]string, numOfFeatures)

	jsonErr := json.Unmarshal(b, &farray)

	if jsonErr == nil {
		tempFeatures := Features(0)
		for _, fstr := range farray {
			feature, err := FeatureFromString(fstr)
			if err != nil {
				return err
			}

			tempFeatures.AddFeature(feature)
		}
		f.AddFeature(tempFeatures)
		return nil
	} else {
		return jsonErr
	}
}
