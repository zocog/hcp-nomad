package semantic

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/sentinel/lang/ast"
	"github.com/hashicorp/sentinel/lang/token"
)

var checkParamPredeclaredIdents = []string{
	"print",
	"undefined",
}

// CheckParamPredeclared validates that a parameter name is not any
// of the set of pre-declared identifiers that are not a normal part
// of the universal scope.
//
// Eval normally catches most items in universe, but some builtins,
// like "print" and "undefined", are not a part of universe and are
// handled in eval as special cases. Hence, they need to be blocked
// here.
type CheckParamPredeclared struct{}

// Check implements Checker for CheckParamImportConflicts.
func (c *CheckParamPredeclared) Check(f *ast.File, fset *token.FileSet) error {
	var result error
	for _, param := range f.Params {
		for _, ident := range checkParamPredeclaredIdents {
			if param.Name.Name == ident {
				result = multierror.Append(result, &CheckError{
					Type:    CheckTypeParamPredeclaredConflict,
					FileSet: fset,
					Pos:     param.Pos(),
					Message: fmt.Sprintf("parameter name cannot be %s", param.Name.Name),
				})
			}
		}
	}

	return result
}
