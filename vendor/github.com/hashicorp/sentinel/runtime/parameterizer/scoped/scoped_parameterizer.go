package scoped

import (
	"fmt"
	"strings"

	"github.com/hashicorp/sentinel/lang/object"
	"github.com/hashicorp/sentinel/runtime/encoding"
)

// ScopedParameterizer is a Parameterizer that will perform a
// prioritized lookup on the underlying data.
//
// The parameter scoping is hierarchal and namespaced, delimited on
// slashes (/). It starts at PATH/KEY, where PATH is the supplied
// Path field, and KEY is the lookup key passed to Lookup.
//
// If the lookup fails at the initial path, it proceeds to
// PARENT/KEY, where PARENT is the parent path of the initially
// supplied path. This continues until the path is empty, the "root",
// at which point the last lookup attempted is just the key itself.
//
// Data is expected to be passed in as a map[string]interface{} of
// unprocessed data. The data must be marshalable to a Sentinel
// object, or else Lookup will return an error.
type ScopedParameterizer struct {
	// The scope path.
	Path string

	// The underlying data.
	Data map[string]interface{}
}

// Lookup implements Parameterizer for ScopedParameterizer.
func (p *ScopedParameterizer) Lookup(key string) (object.Object, error) {
	var k string
	var raw interface{}
	parts := strings.Split(p.Path, "/")
	for i := len(parts); raw == nil && i >= 0; i-- {
		k = strings.Join(append(parts[:i], key), "/")
		raw = p.Data[k]
	}

	if raw == nil {
		// Not found
		return nil, nil
	}

	obj, err := encoding.GoToObject(raw)
	if err != nil {
		return nil, fmt.Errorf("couldn't convert data %q to Sentinel object: %s", k, err)
	}

	return obj, nil
}
