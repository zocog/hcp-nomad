package eval

import "fmt"

// Module represents a module, an AST that is executed within its own scope in
// the interpreter and loaded into a ModuleObj.
//
// Modules are lazily evaluated, and shadow imports. They will not be evaluated
// unless an "import" statement calls them into action. If a module and import
// exist for the same name, the module takes priority.
type Module struct {
	*Compiled
}

// moduleStack is a helper structure that stores a singular, in-flight module
// dependency branch as a stack. This is used to track dependency cycles.
//
// The current Sentinel interpreter model is not concurrent, hence this works -
// modules will always be entered sequentially, even if there are multiple
// edges on a single vertex, ensuring a correct dependency chain is always
// maintained given correct use of EnterModule/ExitModule. If this ever
// changes, a more complex dependency detector will be needed.
//
// This trivial implementation also does not exactly scale too well - we are
// making the assumption here that module depth will not get too large. Again,
// if this changes, we will need something better.
type moduleStack struct {
	paths []string
}

// hasCycle checks the stack for cycles against the supplied path. This is used
// internally by EnterModule and hence is not exported.
func (b *moduleStack) hasCycle(path string) bool {
	for i := len(b.paths) - 1; i > -1; i-- {
		if b.paths[i] == path {
			return true
		}
	}

	return false
}

// EnterModule "enters" a module. For the purposes of tracking, this just means
// that the module is added to the stack. The stack is checked for cycles after
// entering.
func (b *moduleStack) EnterModule(path string) error {
	// Check for cycles and return a error message describing the cycle if one is
	// found.
	if b.hasCycle(path) {
		return fmt.Errorf(
			"import cycle detected\nmodule %s\n%s",
			path,
			b.CycleString(path),
		)
	}

	b.paths = append(b.paths, path)
	return nil
}

// ExitModule deletes the last element off of the stack, effectively removing
// it from the dependency chain.
func (b *moduleStack) ExitModule() {
	b.paths = b.paths[:len(b.paths)-1]
}

// CycleString returns a string representation of the stack as per String, but
// only up to the path supplied. This is used to print only the module cycle
// during an error output.
func (b *moduleStack) CycleString(path string) string {
	if b.paths == nil {
		return ""
	}

	var s string
	for i := len(b.paths) - 1; i > -1; i-- {
		s += fmt.Sprintf("\timports %s\n", b.paths[i])
		if b.paths[i] == path {
			break
		}
	}

	return s
}
