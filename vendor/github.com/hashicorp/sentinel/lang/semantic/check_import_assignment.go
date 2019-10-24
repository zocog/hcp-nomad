package semantic

import (
	"fmt"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/hashicorp/sentinel/lang/ast"
	"github.com/hashicorp/sentinel/lang/token"
)

// CheckImportAssignment enforces no assignments on imports.
//
// This ensures that assignments to imports are blocked on the LHS both
// directly (via direct identifier) or indirectly (via selector or map
// assignment via selector).
type CheckImportAssignment struct{}

// Check implements Checker for CheckImportAssignment.
func (c *CheckImportAssignment) Check(f *ast.File, fset *token.FileSet) error {
	var result error
	for _, stmt := range f.Stmts {
		ast.Rewrite(stmt, func(n ast.Node) ast.Node {
			if n, ok := n.(*ast.AssignStmt); ok {
				// Walk the LHS. Block if we encounter an ident that references an
				// import.
				if err := checkAssignIdent(n.Lhs, f.Imports, fset); err != nil {
					result = multierror.Append(err)
				}

				// Ignore the RHS or the rest of the statement on this check.
				return ast.RewriteSkip(n)
			}

			return n
		})
	}

	return result
}

func checkAssignIdent(n ast.Node, impts []*ast.ImportSpec, fset *token.FileSet) error {
	var result error
	ast.Rewrite(n, func(n ast.Node) ast.Node {
		if n, ok := n.(*ast.IndexExpr); ok {
			// Only check the root of index expressions
			if err := checkAssignIdent(n.X, impts, fset); err != nil {
				result = multierror.Append(err)
			}

			return ast.RewriteSkip(n)
		}

		if n, ok := n.(*ast.Ident); ok {
			for _, impt := range impts {
				if isMatchingImportIdent(impt, n) {
					result = multierror.Append(result, &CheckError{
						Type:    CheckTypeImportAssignProhibited,
						FileSet: fset,
						Pos:     n.NamePos,
						Message: fmt.Sprintf("assignment to import %q prohibited", n.Name),
					})

					break
				}
			}
		}

		return n
	})

	return result
}
