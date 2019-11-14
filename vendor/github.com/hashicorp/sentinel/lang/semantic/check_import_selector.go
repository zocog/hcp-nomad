package semantic

import (
	"fmt"
	"strconv"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/hashicorp/sentinel/lang/ast"
	"github.com/hashicorp/sentinel/lang/token"
)

// CheckImportSelector blocks any bareword reference to an import.
//
// As per the spec, imports are not valid values - they should not be
// assignable nor be able to be referenced by themselves. This check
// walks the AST and blocks both cases on both the LHS and RHS.
type CheckImportSelector struct{}

// Check implements Checker for CheckImportSelector.
func (c *CheckImportSelector) Check(f *ast.File, fset *token.FileSet) error {
	var result error
	for _, stmt := range f.Stmts {
		ast.Rewrite(stmt, func(n ast.Node) ast.Node {
			if _, ok := n.(*ast.SelectorExpr); ok {
				// Our passing case
				return ast.RewriteSkip(n)
			}

			if n, ok := n.(*ast.Ident); ok {
				for _, impt := range f.Imports {
					if isMatchingImportIdent(impt, n) {
						result = multierror.Append(result, &CheckError{
							Type:    CheckTypeImportSelectorRequired,
							FileSet: fset,
							Pos:     n.NamePos,
							Message: fmt.Sprintf("import %q cannot be accessed without a selector expression", n.Name),
						})

						break
					}
				}
			}

			return n
		})
	}

	return result
}

// isMatchingImportIdent checks to see if the identifier specified by
// ident matches the import specified by import. It does this by
// checking the name if it exists, falling back to the path
// otherwise.
func isMatchingImportIdent(impt *ast.ImportSpec, ident *ast.Ident) bool {
	if impt.Name != nil {
		return impt.Name.Name == ident.Name
	}

	path, err := strconv.Unquote(impt.Path.Value)
	if err != nil {
		return false
	}

	return path == ident.Name
}
