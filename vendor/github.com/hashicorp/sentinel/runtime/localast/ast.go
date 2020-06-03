package localast

import (
	"github.com/hashicorp/sentinel/lang/ast"
	"github.com/hashicorp/sentinel/lang/token"
)

// ImportExpr is an ast.Node that represents a potential import field access
// and optionally a function call. It is potentially an import access and not
// definitely because the Import field may be shadowed by a variable
// assignment. In this case, the Original expression should be evaluated.
type ImportExpr struct {
	Original         ast.Node    `ast:"norewrite"` // original in case import is shadowed
	StartPos, EndPos token.Pos   // position of the selector and end
	Import           string      // Import being accessed
	Keys             []ImportKey // Selector keys and appropriate args (for calls)
}

type ImportKey struct {
	Key  string
	Args []ast.Expr
}

func (n *ImportExpr) Pos() token.Pos { return n.StartPos }
func (n *ImportExpr) End() token.Pos { return n.EndPos }
func (n *ImportExpr) ExprNode()      {}

// printer.Printable
func (n *ImportExpr) PrintNode() ast.Node { return n.Original }
