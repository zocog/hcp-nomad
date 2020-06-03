package semantic

import (
	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/sentinel/lang/ast"
	"github.com/hashicorp/sentinel/lang/token"
)

// CheckAppend is a checker to verify that all usages of append
// are not looking for a return value
type CheckAppend struct{}

func (c *CheckAppend) Check(f *ast.File, fset *token.FileSet) error {
	var result error
	for _, stmt := range f.Stmts {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok {
			continue
		}

		call, ok := assign.Rhs.(*ast.CallExpr)
		if !ok {
			continue
		}

		ident, ok := call.Fun.(*ast.Ident)
		if !ok {
			continue
		}

		if ident.Name == "append" {
			result = multierror.Append(result, &CheckError{
				Type:    CheckTypeAppendAssign,
				FileSet: fset,
				Pos:     assign.Lhs.Pos(),
				Message: `"append" was used in a way that expected a return value. The value of "append" is undefined`,
			})

		}
	}

	return result
}
