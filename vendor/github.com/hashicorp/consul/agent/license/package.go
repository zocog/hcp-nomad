// +build consulent

package license

import (
	"fmt"
	"strings"
)

// Package holds a package of features
type Package uint

const (
	// PackageNone means no feature package
	PackageNone Package = iota
	// PackagePro mean professional package
	PackagePro
	// PackagePremium means all premium features
	PackagePremium

	// Should always be last, add packages above
	numOfPackages int = iota
)

// String returns the string for a given flag
func (p Package) String() string {
	switch p {
	case PackageNone:
		return "none"
	case PackagePremium:
		return "premium"
	case PackagePro:
		return "pro"
	default:
		return "unknown"
	}
}

// AllPackages returns a list of all available features
func AllPackages() []Package {
	packages := make([]Package, numOfPackages)
	for i := 0; i < numOfPackages; i++ {
		packages[i] = Package(i)
	}
	return packages
}

// PackageFromString convert the package name string to a Package
func PackageFromString(pkg string) (Package, error) {
	switch strings.ToLower(pkg) {
	case "", "none":
		return PackageNone, nil
	case "premium":
		return PackagePremium, nil
	case "pro":
		return PackagePro, nil
	default:
		return PackageNone, fmt.Errorf("invalid package: %q", pkg)
	}
}

// Features returns all the features for a package
func (p Package) Features() Features {
	switch p {
	case PackagePremium:
		return AllFeatures()

	case PackagePro:
		return FeatureAutoBackups | FeatureAutoUpgrades | FeatureReadScalability | FeatureNetworkSegments

	default:
		return FeatureNone
	}
}
