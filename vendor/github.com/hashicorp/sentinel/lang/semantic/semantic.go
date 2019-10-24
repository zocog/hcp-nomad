// Package semantic contains runtime-agnostic semantic checks on a parsed
// AST. This verifies semantic behavior such as `main` existing, functions
// having a return statement, and more.
//
// All semantic checks made here are according to the language specification.
// For runtime-specific behavior, a runtime may want to add their own checks.
package semantic

import (
	"reflect"

	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/sentinel/lang/ast"
	"github.com/hashicorp/sentinel/lang/token"
)

// CheckOpts provides options for semantic checks.
type CheckOpts struct {
	File    *ast.File
	FileSet *token.FileSet

	// SkipCheckers is a list of checkers that should be skipped.
	SkipCheckers []Checker
}

// Checker is the interface that each checker implements.
type Checker interface {
	Check(*ast.File, *token.FileSet) error
}

// defaultChecks defines the default list of currently enabled
// checks.
var defaultChecks = []Checker{
	&CheckMain{},
	&CheckImportSpec{},
	&CheckImportSelector{},
	&CheckImportAssignment{},
	&CheckBreak{},
	&CheckAppend{},
}

func matchCheck(checkers []Checker, match Checker) bool {
	for _, check := range checkers {
		if reflect.ValueOf(check).Type() == reflect.ValueOf(match).Type() {
			return true
		}
	}

	return false
}

// Check performs semantic checks on the given file.
//
// The returned error may be multiple errors contained within a go-multierror
// structure. You can type assert on the result to extract individual errors.
func Check(opts CheckOpts) error {
	var err *multierror.Error
	for _, check := range defaultChecks {
		if matchCheck(opts.SkipCheckers, check) {
			continue
		}

		if e := check.Check(opts.File, opts.FileSet); e != nil {
			err = multierror.Append(err, e)
		}
	}

	return err.ErrorOrNil()
}
