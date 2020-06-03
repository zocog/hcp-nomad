package ast

import (
	"strconv"
)

// Imports returns the list of unquoted imports that the file contains.
// Note that the import values may still be invalid since this performs
// no semantic checks.
func Imports(f *File) []string {
	imports := make([]string, len(f.Imports))
	for i, spec := range f.Imports {
		// Note we purposely ignore errors because checking the value of
		// the import is explicitly not the responsibility of this function.
		// The validity of import syntax is checked in the parser.
		s, _ := strconv.Unquote(spec.Path.Value)
		imports[i] = s
	}

	return imports
}
