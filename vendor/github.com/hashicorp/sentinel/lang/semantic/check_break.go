package semantic

import (
	"fmt"

	"github.com/hashicorp/go-multierror"

	"github.com/hashicorp/sentinel/lang/ast"
	"github.com/hashicorp/sentinel/lang/token"
)

// CheckBreak verifies that break and continue only exist within
// a `for` loop.
type CheckBreak struct{}

// Check implements Checker for CheckBreak.
func (c *CheckBreak) Check(f *ast.File, fset *token.FileSet) error {
	var result error
	ast.Rewrite(f, func(n ast.Node) ast.Node {
		// If it is a for, ignore the value.
		if _, ok := n.(*ast.ForStmt); ok {
			return ast.RewriteSkip(n)
		}

		if n, ok := n.(*ast.BranchStmt); ok {
			result = multierror.Append(result, &CheckError{
				Type:    CheckTypeBreak,
				FileSet: fset,
				Pos:     n.Pos(),
				Message: fmt.Sprintf("`%s` found outside of a for loop", n.Tok),
			})
		}

		return n
	})

	return result
}
