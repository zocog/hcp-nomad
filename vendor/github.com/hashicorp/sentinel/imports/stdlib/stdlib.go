// Package stdlib has the list of "stdlib" imports with their factory functions.
package stdlib

import (
	sdk "github.com/hashicorp/sentinel-sdk"

	i_decimal "github.com/hashicorp/sentinel/imports/decimal"
	i_json "github.com/hashicorp/sentinel/imports/json"
	i_runtime "github.com/hashicorp/sentinel/imports/runtime"
	i_sockaddr "github.com/hashicorp/sentinel/imports/sockaddr"
	i_strings "github.com/hashicorp/sentinel/imports/strings"
	i_time "github.com/hashicorp/sentinel/imports/time"
	i_types "github.com/hashicorp/sentinel/imports/types"
	i_units "github.com/hashicorp/sentinel/imports/units"
)

// Imports is the map of built-in imports.
//
// Note that these imports may require further configuration. It is up to the
// user of this map to ensure they're properly configured or error appropriately
// at runtime.
//
// Additionally, no guarantee is made that each of these libraries is "safe"
// to enable by default. Each embedding application should determine for itself
// whether it should enable an import by default.
var Imports = map[string]func() sdk.Import{
	"decimal":  i_decimal.New,
	"json":     i_json.New,
	"runtime":  i_runtime.New,
	"sockaddr": i_sockaddr.New,
	"strings":  i_strings.New,
	"time":     i_time.New,
	"types":    i_types.New,
	"units":    i_units.New,
}
