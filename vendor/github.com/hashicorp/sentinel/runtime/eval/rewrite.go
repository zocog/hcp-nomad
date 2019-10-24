package eval

import (
	"github.com/hashicorp/sentinel/lang/ast"
)

// rewriteImportSelector is an ast rewrite function that detects selector
// expressions that are import accesses and rewrites them to use astImportExpr.
func rewriteImportSelector() func(ast.Node) ast.Node {
	// Predeclare the function so that we can recursively call it within itself
	var f func(n ast.Expr) ast.Expr
	f = func(n ast.Expr) ast.Expr {
		// If this is a call expression then determine if it is an import call.
		if cx, ok := n.(*ast.CallExpr); ok {
			// Rewrite the function expression. If it turns into an import
			// expression then this is an import function call. We only have
			// to modify the import expression slightly to note it is a call.
			//
			// The original needs to be reset back to the function call expression to
			// ensure that function calls can succeed on selectors, which generally
			// happens during module calls. This may not be 100% optimized and may be
			// moved out of this part.
			if ix, ok := f(cx.Fun).(*astImportExpr); ok {
				// Go over each arg and rewrite if appropriate.
				args := make([]ast.Expr, len(cx.Args))
				for i, arg := range cx.Args {
					args[i] = f(arg)
				}

				ix.EndPos = cx.End()                // EndPos is now the end of the call
				ix.Keys[len(ix.Keys)-1].Args = args // Populate args
				ix.Original = cx                    // Rewrite the original back to the call expression
				return ix                           // Return the import expression itself
			}
		}

		// Logic depends on inbound expression type
		switch x := n.(type) {
		case *ast.UnaryExpr:
			// Traverse unary expressions
			return &ast.UnaryExpr{
				OpPos: x.OpPos,
				Op:    x.Op,
				X:     f(x.X),
			}

		case *ast.BinaryExpr:
			// Traverse both expressions in a binary expression
			return &ast.BinaryExpr{
				X:     f(x.X),
				OpPos: x.OpPos,
				Op:    x.Op,
				OpNeg: x.OpNeg,
				Y:     f(x.Y),
			}

		case *ast.SelectorExpr:
			// Otherwise we operate on selectors.
			//
			// If the root is an ident, this is our top selector. Return an
			// initial state.
			if ident, ok := x.X.(*ast.Ident); ok {
				return &astImportExpr{
					Original: x,
					StartPos: x.Pos(),
					EndPos:   x.End(),
					Import:   ident.Name,
					Keys:     []astImportKey{{Key: x.Sel.Name}},
				}
			}

			// Otherwise, we check to see if we can get an import expression
			// by recursion.
			if ix, ok := f(x.X).(*astImportExpr); ok {
				// Update various fields of the import expression and return
				// it.
				ix.Original = x                                          // Original is the breadth of the expression so far
				ix.StartPos = x.Pos()                                    // Update start
				ix.EndPos = x.End()                                      // Update end
				ix.Keys = append(ix.Keys, astImportKey{Key: x.Sel.Name}) // Add root expression ident as key

				return ix
			}

			// Otherwise, just return the original selector expr
			return x
		}

		// Unsupported type for rewriting, just return
		return n
	}

	return func(n ast.Node) ast.Node {
		if x, ok := n.(ast.Expr); ok {
			return f(x)
		}

		return n
	}
}
