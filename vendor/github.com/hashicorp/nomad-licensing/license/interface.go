package license

import (
	"encoding/json"
	"sync"

	"github.com/hashicorp/go-licensing/flags"
)

const ProductName = "nomad"

// NomadFlags is an empty struct used to satisfy the ProductFlags
// interface in go-licensing
type NomadFlags struct {
	mu           sync.Mutex
	flagsOptions *flags.FlagsOptions
}

// Product returns the name of this product
func (nf *NomadFlags) Product() string {
	return ProductName
}

// Parse takes a map of strings to interfaces and returns a Consul-specific
// flags configuration
func (nf *NomadFlags) Parse(flgs map[string]interface{}) (interface{}, error) {
	b, err := json.Marshal(flgs)
	if err != nil {
		return nil, err
	}

	var flags Flags
	if err := json.Unmarshal(b, &flags); err != nil {
		return nil, err
	}
	return flags, nil
}

func getFlagFeatures() []flags.Feature {
	features := allFeatures().List()
	ifaceFeatures := make([]flags.Feature, len(features))
	for x, f := range features {
		ifaceFeatures[x] = flags.Feature{
			Name: f.String(),
		}
	}
	return ifaceFeatures
}

func getFlagModules() []flags.Module {
	return nil
}

// DescribeFlags returns a type that describes all available
// options for a nomad-specific flags configuration
func (nf *NomadFlags) DescribeFlags() flags.FlagsOptions {
	nf.mu.Lock()
	defer nf.mu.Unlock()

	if nf.flagsOptions != nil {
		return *nf.flagsOptions
	}

	// Load modules
	modules := make([]flags.Module, numOfModules)
	for i := 0; i < numOfModules; i++ {
		modules[i] = flags.Module{Name: Module(i + 1).String()}
	}

	// Load features
	allFeatures := AllFeatures().List()
	features := make([]flags.Feature, len(allFeatures))
	for i, f := range allFeatures {
		features[i] = flags.Feature{Name: f.String()}
	}

	nf.flagsOptions = &flags.FlagsOptions{
		Modules:  modules,
		Features: features,
	}
	return *nf.flagsOptions
}
