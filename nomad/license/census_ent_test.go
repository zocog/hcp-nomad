//go:build ent
// +build ent

package license

import (
	"testing"

	census "github.com/hashicorp/go-census/schema"
	"github.com/hashicorp/nomad/ci"
	"github.com/shoenig/test/must"
)

func TestNewCensusSchema_Validate(t *testing.T) {
	ci.Parallel(t)
	schema := NewCensusSchema()

	result, err := census.Validate(schema)
	must.NoError(t, err)
	must.True(t, result)
}
