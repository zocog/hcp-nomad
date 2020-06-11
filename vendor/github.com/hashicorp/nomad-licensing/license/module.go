package license

import (
	"fmt"
	"strings"
)

// Module holds a set of features
type Module uint

// ModuleUnknown is a module that is unknown and contains no features
const ModuleUnknown Module = 0

const (
	// ModuleGovernancePolicy
	ModuleGovernancePolicy Module = iota + 1

	ModulePlatform

	// Should always be last, add modules above
	numOfModules int = iota
)

func (m Module) String() string {
	switch m {
	case ModuleGovernancePolicy:
		return "governance-policy"
	case ModulePlatform:
		return "platform"
	default:
		return "unknown"
	}
}

// ModuleFromString convert the module name string to a Module
func ModuleFromString(module string) (Module, error) {
	switch strings.ToLower(module) {
	case "governance-policy":
		return ModuleGovernancePolicy, nil
	case "platform":
		return ModulePlatform, nil
	default:
		return ModuleUnknown, fmt.Errorf("invalid module: %q", module)
	}
}

// ModulePlatform represents the features that are delivered in all versions of Vault Enterprise
const ModulePlatformFeatures Features = FeatureAutoUpgrades | FeatureReadScalability | FeatureRedundancyZones | FeatureMultiregionDeployments

// Features returns all the features for a module
func (m Module) Features() Features {
	switch m {
	case ModuleGovernancePolicy:
		return ModulePlatformFeatures | FeatureNamespaces | FeatureResourceQuotas | FeaturePreemption | FeatureAuditLogging | FeatureSentinelPolicies

	case ModulePlatform:
		fallthrough
	default:
		return ModulePlatformFeatures
	}
}
