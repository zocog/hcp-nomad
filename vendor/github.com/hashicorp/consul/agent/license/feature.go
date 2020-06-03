// +build consulent

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
	// consul snapshot agent
	FeatureAutoBackups Features = 1 << iota

	// autopiolot consul upgrades
	FeatureAutoUpgrades

	// non-voting servers
	FeatureReadScalability

	// Network Segments
	FeatureNetworkSegments

	// autopilot redundancy zones
	FeatureRedundancyZone

	// network areas
	FeatureAdvancedFederation

	// namespaces
	FeatureNamespaces

	// Should always be last, add features above
	numOfFeatures uint = iota
)

// String returns the string for a given flag
func (f Features) String() string {
	switch f {
	case FeatureAutoBackups:
		return "Automated Backups"
	case FeatureAutoUpgrades:
		return "Automated Upgrades"
	case FeatureReadScalability:
		return "Enhanced Read Scalability"
	case FeatureNetworkSegments:
		return "Network Segments"
	case FeatureRedundancyZone:
		return "Redundancy Zone"
	case FeatureAdvancedFederation:
		return "Advanced Network Federation"
	case FeatureNamespaces:
		return "Namespaces"
	default:
		return "Unknown"
	}
}

// FeatureFromString returns the feature flag for the given string
// representation
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

// AllFeatures returns a list of all available features, excluding deprecated
// features
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

// RemoveFeature adds a feature
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
