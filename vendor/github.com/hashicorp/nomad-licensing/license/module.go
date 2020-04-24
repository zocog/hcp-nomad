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
	ModuleGovernancePolicy = iota + 1

	// Should always be last, add modules above
	numOfModules int = iota
)

func (m Module) String() string {
	switch m {
	case ModuleGovernancePolicy:
		return "governance-policy"
	default:
		return "unknown"
	}
}

// ModuleFromString convert the module name string to a Module
func ModuleFromString(module string) (Module, error) {
	switch strings.ToLower(module) {
	case "governance-policy":
		return ModuleGovernancePolicy, nil
	default:
		return ModuleUnknown, fmt.Errorf("invalid module: %q", module)
	}
}

// ModulePlatform represents the features that are delivered in all versions of Vault Enterprise
const ModulePlatform Features = FeatureAutoUpgrades | FeatureReadScalability | FeatureRedundancyZones

// Features returns all the features for a module
func (m Module) Features() Features {
	switch m {
	case ModuleGovernancePolicy:
		return ModulePlatform | FeatureNamespaces | FeatureResourceQuotas | FeaturePreemption | FeatureAuditLogging | FeatureSetinelPolicies

	default:
		return ModulePlatform
	}
}
