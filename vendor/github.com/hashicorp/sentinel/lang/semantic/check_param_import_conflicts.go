package semantic

import (
	"fmt"
	"strconv"

	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/sentinel/lang/ast"
	"github.com/hashicorp/sentinel/lang/token"
)

// CheckParamImportConflicts validates that a parameter name does not
// conflict with an existing import.
type CheckParamImportConflicts struct{}

// Check implements Checker for CheckParamImportConflicts.
func (c *CheckParamImportConflicts) Check(f *ast.File, fset *token.FileSet) error {
	var result error
	for _, param := range f.Params {
		for _, impt := range f.Imports {
			var name string
			if impt.Name != nil {
				name = impt.Name.Name
			} else {
				var err error
				name, err = strconv.Unquote(impt.Path.Value)
				if err != nil {
					// This is an error parsing the import path, more than
					// likely will never happen, but if it does, it's a
					// CheckTypeImportBadPath error, not a param error.
					result = multierror.Append(result, &CheckError{
						Type:    CheckTypeImportBadPath,
						FileSet: fset,
						Pos:     impt.Path.Pos(),
						Message: fmt.Sprintf("invalid import path %q", impt.Path.Value),
					})
				}
			}

			if name == param.Name.Name {
				result = multierror.Append(result, &CheckError{
					Type:    CheckTypeParamImportConflict,
					FileSet: fset,
					Pos:     param.Name.Pos(),
					Message: fmt.Sprintf("parameter name %q already exists as an import", param.Name.Name),
				})
			}
		}
	}

	return result
}
