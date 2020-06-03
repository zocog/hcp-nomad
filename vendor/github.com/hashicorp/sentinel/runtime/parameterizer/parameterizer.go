package parameterizer

import "github.com/hashicorp/sentinel/lang/object"

// Parameterizer is an interface that will load a parameter value of
// a specific name. The purpose is allow higher-level APIs to supply
// a way to load parameter defaults while allowing for facilities
// like precedence, etc.
type Parameterizer interface {
	// Lookup takes a parameter name and returns an object value. The
	// implementation is responsible for any conversion to an object.
	//
	// To denote that a value was not found, Lookup should return nil
	// as the object, in addition to a nil error.
	Lookup(string) (object.Object, error)
}
