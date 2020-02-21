// +build consulent

package license

import (
	"encoding/json"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/lib"
	"github.com/hashicorp/go-licensing"
	"github.com/mitchellh/copystructure"
)

// License contains vault specific extensions to the license
type License struct {
	*licensing.License
	Package   Package  `json:"-"`
	Features  Features `json:"features"`
	Temporary bool     `json:"temporary"`
}

// NewLicense wraps a general license with vault specific features
func NewLicense(license *licensing.License) (*License, error) {
	flags, err := parseFlags(license.Flags)
	if err != nil {
		return nil, err
	}

	return &License{
		License:   license,
		Package:   flags.Package,
		Features:  flags.features(),
		Temporary: flags.Temporary,
	}, nil
}

func (lic *License) ToMap() map[string]string {
	m := make(map[string]string)
	if lic != nil {
		m["id"] = lic.LicenseID
		m["customer"] = lic.CustomerID
		m["install_id"] = lic.InstallationID
		m["issue_time"] = lic.IssueTime.String()
		m["start_time"] = lic.StartTime.String()
		m["expiration_time"] = lic.ExpirationTime.String()
		m["product"] = lic.Product
		if pkg, ok := lic.Flags["package"].(string); ok {
			m["package"] = pkg
		}
		features := ""
		first := true

		for _, f := range lic.Features.List() {
			if !first {
				features += ", "
			} else {
				first = false
			}
			features += f.String()
		}
		m["features"] = features
	}

	return m
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

func (l *License) MarshalJSON() ([]byte, error) {
	type typeCopy License
	copy := typeCopy(*l)

	// If we have flags, then we want to run it through our flagsWalker
	// wich is a reflectwalk implementation that attempts to turn arbitrary
	// interface{} values into JSON-safe equivalents (more or less). This
	// should always work because the flags were originally encoded in JSON
	// within the license blob
	if copy.Flags != nil {
		var err error
		copy.Flags, err = lib.MapWalk(copy.Flags)
		if err != nil {
			return nil, err
		}
	}

	return json.Marshal(&copy)
}

func (l *License) ToAPILicense() *api.License {
	return &api.License{
		LicenseID:       l.LicenseID,
		CustomerID:      l.CustomerID,
		InstallationID:  l.InstallationID,
		IssueTime:       l.IssueTime,
		StartTime:       l.StartTime,
		ExpirationTime:  l.ExpirationTime,
		TerminationTime: l.TerminationTime,
		Product:         l.Product,
		Flags:           l.Flags,
		Features:        l.Features.StringList(),
	}
}

type featureFlags struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

type flagsRaw struct {
	Package   string       `json:"package"`
	Features  featureFlags `json:"features"`
	Temporary bool         `json:"temporary"`
}

type flags struct {
	Package   Package
	Add       Features
	Remove    Features
	Temporary bool
}

func parseFlags(flagMap map[string]interface{}) (*flags, error) {
	flagsJSON, err := json.Marshal(flagMap)
	if err != nil {
		return nil, err
	}

	var raw flagsRaw

	if err := json.Unmarshal(flagsJSON, &raw); err != nil {
		return nil, err
	}

	addFeatures := Features(0)
	rmFeatures := Features(0)
	pkg, err := PackageFromString(raw.Package)
	if err != nil {
		return nil, err
	}

	for _, f := range raw.Features.Add {
		feature, err := FeatureFromString(f)
		if err != nil {
			return nil, err
		}
		addFeatures.AddFeature(feature)
	}

	for _, f := range raw.Features.Remove {
		feature, err := FeatureFromString(f)
		if err != nil {
			return nil, err
		}
		rmFeatures.AddFeature(feature)
	}

	return &flags{Package: pkg, Add: addFeatures, Remove: rmFeatures, Temporary: raw.Temporary}, nil
}

func (f *flags) features() Features {
	resolved := f.Package.Features()
	resolved.AddFeature(f.Add)
	resolved.RemoveFeature(f.Remove)
	return resolved
}
