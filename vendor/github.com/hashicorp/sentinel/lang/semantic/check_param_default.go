package semantic

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/sentinel/lang/ast"
	"github.com/hashicorp/sentinel/lang/token"
)

// CheckParamDefault validates that the default in a parameter follows
// the restrictions laid out in the spec, which only allows primitive
// types, and list and map. This means that any AST type that denotes
// a non-primitive, list, map, or boolean value is not allowed.
type CheckParamDefault struct {
	fset *token.FileSet
}

// Check implements Checker for CheckParamDefault.
func (c *CheckParamDefault) Check(f *ast.File, fset *token.FileSet) error {
	var result error
	c.fset = fset

	for _, param := range f.Params {
		if param.Default != nil {
			result = multierror.Append(result, c.walkExpr(param.Default))
		}
	}

	return result
}

func (c *CheckParamDefault) walkExpr(e ast.Expr) error {
	var errs error
	switch x := e.(type) {
	case *ast.BasicLit:
		// All basic literals are permitted

	case *ast.UnaryExpr:
		// While not in the spec per se, we don't allow "!" or "not",
		// which are valid operators in unary expressions, but don't
		// really represent anything in a literal, as opposed to infix
		// "-" and "+". In this case, we assume it's an expression and
		// add the CheckTypeDefaultTypeProhibited error.
		if x.Op != token.ADD && x.Op != token.SUB {
			errs = multierror.Append(errs, &CheckError{
				Type:    CheckTypeParamDefaultTypeProhibited,
				FileSet: c.fset,
				Pos:     x.Pos(),
				Message: "invalid expression for parameter default, must be a basic literal, boolean, list, or map",
			})
		} else {
			errs = multierror.Append(c.walkExpr(x.X))
		}

	case *ast.Ident:
		// Only "true" and "false" are allowed.
		if x.Name != "true" && x.Name != "false" {
			errs = multierror.Append(errs, &CheckError{
				Type:    CheckTypeParamDefaultIdentProhibited,
				FileSet: c.fset,
				Pos:     x.Pos(),
				Message: fmt.Sprintf("identifier %q not allowed as parameter default", x.Name),
			})
		}

	case *ast.ListLit:
		// Walk elements.
		for _, elt := range x.Elts {
			errs = multierror.Append(c.walkExpr(elt))
		}

	case *ast.MapLit:
		// Walk both keys and values of elements.
		for _, elt := range x.Elts {
			errs = multierror.Append(errs, c.walkExpr(elt.(*ast.KeyValueExpr).Key))
			errs = multierror.Append(errs, c.walkExpr(elt.(*ast.KeyValueExpr).Value))
		}

	default:
		// Not a valid type for a default at all
		errs = multierror.Append(errs, &CheckError{
			Type:    CheckTypeParamDefaultTypeProhibited,
			FileSet: c.fset,
			Pos:     e.Pos(),
			Message: "invalid expression for parameter default, must be a basic literal, boolean, list, or map",
		})
	}

	return errs
}
