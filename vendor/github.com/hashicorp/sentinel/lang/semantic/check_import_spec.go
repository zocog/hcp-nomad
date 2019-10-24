package semantic

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/hashicorp/sentinel/lang/ast"
	"github.com/hashicorp/sentinel/lang/parser"
	"github.com/hashicorp/sentinel/lang/token"
)

// CheckImportSpec validates an import expression to ensure that it
// has both a valid path for importing, and that it will be able to
// be used:
//
// * Illegal characters are blocked
// * If the import contains legal characters that cannot be used as
// an identifier, we ensure that "as" is set
type CheckImportSpec struct{}

// Check implements Checker for CheckImportSpec.
func (c *CheckImportSpec) Check(f *ast.File, fset *token.FileSet) error {
	var result *multierror.Error
	for _, impt := range f.Imports {
		if !isValidImport(impt.Path.Value) {
			result = multierror.Append(result, &CheckError{
				Type:    CheckTypeImportBadPath,
				FileSet: fset,
				Pos:     impt.Path.ValuePos,
				Message: fmt.Sprintf("invalid import path %q", impt.Path.Value),
			})

			continue
		}

		if !isValidImportIdent(impt.Path.Value) && impt.Name == nil {
			result = multierror.Append(result, &CheckError{
				Type:    CheckTypeImportIdentRequired,
				FileSet: fset,
				Pos:     impt.Path.ValuePos,
				Message: fmt.Sprintf(`import path %q cannot be used as an identifier; use the "as" clause to specify a local name for this import`, impt.Path.Value),
			})

			continue
		}
	}

	return result
}

func isValidImport(lit string) bool {
	const illegalChars = `!"#$%&'()*,:;<=>?[\]^{|}` + "`\uFFFD"
	s, _ := strconv.Unquote(lit) // go/scanner returns a legal string literal
	for _, r := range s {
		if !unicode.IsGraphic(r) || unicode.IsSpace(r) || strings.ContainsRune(illegalChars, r) {
			return false
		}
	}
	return s != ""
}

// isValidImportIdent checks to see if lit can be parsed as an
// identifier. Errors are treated as false.
func isValidImportIdent(lit string) bool {
	path, err := strconv.Unquote(lit)
	if err != nil {
		return false
	}

	expr, err := parser.ParseExpr(path)
	if err != nil {
		return false
	}

	_, ok := expr.(*ast.Ident)
	return ok
}
