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

	ModuleMulticlusterAndEfficiency

	// Should always be last, add modules above
	numOfModules int = iota
)

func (m Module) String() string {
	switch m {
	case ModuleGovernancePolicy:
		return "governance-policy"
	case ModuleMulticlusterAndEfficiency:
		return "multicluster-and-efficiency"
	default:
		return "unknown"
	}
}

// ModuleFromString convert the module name string to a Module
func ModuleFromString(module string) (Module, error) {
	switch strings.ToLower(module) {
	case "governance-policy":
		return ModuleGovernancePolicy, nil
	case "multicluster-and-efficiency":
		return ModuleMulticlusterAndEfficiency, nil
	default:
		return ModuleUnknown, fmt.Errorf("invalid module: %q", module)
	}
}

// ModulePlatform represents the features that are delivered in all versions of Nomad Enterprise
const (
	PlatformFeatures Features = FeatureAutoUpgrades | FeatureReadScalability |
		FeatureRedundancyZones | FeatureAutoBackups | FeatureMultiVaultNamespaces
)

// Features returns all the features for a module
func (m Module) Features() Features {
	switch m {
	case ModuleGovernancePolicy:
		return PlatformFeatures | FeatureNamespaces | FeatureResourceQuotas | FeatureAuditLogging | FeatureSentinelPolicies
	case ModuleMulticlusterAndEfficiency:
		return PlatformFeatures | FeatureMultiregionDeployments
	default:
		return PlatformFeatures
	}
}
