package license

import (
	"encoding/json"

	"github.com/hashicorp/go-licensing"
	"github.com/mitchellh/copystructure"
)

// License contains nomad specific extensions to the license
type License struct {
	*licensing.License
	Features  Features `json:"features"`
	Temporary bool     `json:"temporary"`
}

func NewLicense(license *licensing.License) (*License, error) {
	flagsRaw, err := json.Marshal(license.Flags)
	if err != nil {
		return nil, err
	}

	// Populate LicenseFlags struct

	var flags Flags
	if err := json.Unmarshal(flagsRaw, &flags); err != nil {
		return nil, err
	}

	return &License{
		License:   license,
		Features:  flags.features,
		Temporary: flags.Temporary,
	}, nil
}

// Clone creates a copy of the license
func (l *License) Clone() (*License, error) {
	lic, err := copystructure.Copy(l)
	if err != nil {
		return nil, err
	}
	licenseCopy := lic.(*License)
	licenseCopy.Features = l.Features
	return licenseCopy, nil
}

// HasFeature if the license has a feature
func (l *License) HasFeature(feature Features) bool {
	return l.Features.HasFeature(feature)
}

type featureFlags struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

type flagsRaw struct {
	Modules   []string  `json:"modules"`
	Features  *features `json:"features"`
	Temporary bool      `json:"temporary"`
}

type Flags struct {
	flagsRaw
	features  Features
	Temporary bool
}

// UnmarshalJSON is a custom unmarshaller for LicenseFlags
func (f *Flags) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &f.flagsRaw); err != nil {
		return err
	}

	// Add Platform features to feature flags
	f.features.AddFeature(ModulePlatform)

	// Iterate over modules
	for _, modRaw := range f.Modules {
		mod, err := ModuleFromString(modRaw)
		if err != nil {
			return err
		}
		f.features.AddFeature(mod.Features())
	}

	if f.Features != nil {
		f.features.AddFeature(f.Features.add)
		f.features.RemoveFeature(f.Features.remove)
	}

	return nil
}

// FeaturesRaw is the feature struct in the license flags
type featuresRaw struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

type features struct {
	featuresRaw
	add    Features
	remove Features
}

// UnmarshalJSON is a custom unmarshaller for Features
func (f *features) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &f.featuresRaw); err != nil {
		return err
	}
	for _, a := range f.Add {
		feature, err := FeatureFromString(a)
		if err != nil {
			return err
		}

		f.add.AddFeature(feature)
	}
	for _, r := range f.Remove {
		feature, err := FeatureFromString(r)
		if err != nil {
			return err
		}

		f.remove.AddFeature(feature)
	}
	return nil
}
