// Package eval contains an evaluator for the Sentinel policy language.
package eval

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	sdk "github.com/hashicorp/sentinel-sdk"
	"github.com/hashicorp/sentinel/lang/ast"
	"github.com/hashicorp/sentinel/lang/object"
	"github.com/hashicorp/sentinel/lang/token"
	"github.com/hashicorp/sentinel/runtime/encoding"
	"github.com/hashicorp/sentinel/runtime/importer"
	"github.com/hashicorp/sentinel/runtime/trace"
)

var mapStringInterfaceTyp = reflect.TypeOf(map[string]interface{}{})

type EvalOpts struct {
	Compiled *Compiled          // Compiled policy to evaluate
	Scope    *object.Scope      // Global scope, the parent of this should be Universe
	Importer importer.Importer  // Importer for imports
	Modules  map[string]*Module // Available modules
	Timeout  time.Duration      // Max execution time
	Trace    *trace.Trace       // Set to non-nil to recording tracing information
}

// Eval evaluates the compiled policy and returns the result.
//
// err may be an "UndefError" which specifically represents than an "undefined"
// value bubbled all the way to the final result of the policy. This usually
// represents erroneous policy logic.
//
// err may also be "EvalError" which contains more details about the
// environment that resulted in the error.
func Eval(opts *EvalOpts) (bool, error) {
	// Build the evaluation state
	state := &evalState{
		ExecId:   atomic.AddUint64(&globalExecId, 1),
		File:     opts.Compiled.file,
		FileSet:  opts.Compiled.fileSet,
		Scope:    opts.Scope,
		Importer: opts.Importer,
		Modules:  opts.Modules,
		Timeout:  opts.Timeout,
		Trace:    opts.Trace,
	}

	result, err := state.Eval()

	// If we're tracing, record the final results
	if state.Trace != nil {
		state.Trace.Desc = opts.Compiled.file.Doc.Text()
		state.Trace.Result = result
		state.Trace.Err = err

		// If there is no policy docstring, default to the main rule (if
		// that has a docstring)
		if state.Trace.Desc == "" {
			if r, ok := state.Trace.Rules["main"]; ok && r.Desc != "" {
				state.Trace.Desc = r.Desc
			}
		}
	}

	return result, err
}

// globalExecId is the global execution ID. If an execution ID pointer
// isn't given in EvalOpts, this ID is incremented and used.
var globalExecId uint64

// These are special runtime objects that we use as sentinel values as part
// of runtime execution.
var (
	runtimeBreakObj    = &object.RuntimeObj{}
	runtimeContinueObj = &object.RuntimeObj{}
)

// Interpreter is the interpreter for Sentinel, implemented using a basic
// AST walk. After calling any methods of Interpreter, the exported fields
// should not be modified.
type evalState struct {
	ExecId   uint64             // Execution ID, unique per evaluation
	File     *ast.File          // File to execute
	FileSet  *token.FileSet     // FileSet for positional information
	MaxDepth int                // Maximum stack depth
	Scope    *object.Scope      // Global scope, the parent of this should be Universe
	Importer importer.Importer  // Importer for imports
	Modules  map[string]*Module // Available modules
	Timeout  time.Duration      // Timeout for execution
	Trace    *trace.Trace       // If non-nil, sets trace data here

	deadline    time.Time             // deadline of this execution
	depth       int                   // current stack depth
	imports     map[string]sdk.Import // imports are loaded imports
	moduleStack moduleStack           // module stack for tracking import loops
	returnObj   object.Object         // object set by last `return` statement
	timeoutCh   <-chan time.Time      // closed after a timeout is reached

	// Trace data

	ruleMap    map[token.Pos]token.Pos   // map from expr position to assignment
	traces     map[token.Pos]*trace.Rule // rule traces
	traceStack []*trace.Bool             // current trace, nil if no trace active
}

// Eval evaluates the policy and returns the resulting value.
//
// err may be an "UndefError" which specifically represents than an "undefined"
// value bubbled all the way to the final result of the policy. This usually
// represents erroneous policy logic.
//
// err may also be "EvalError" which contains more details about the
// environment that resulted in the error.
func (e *evalState) Eval() (result bool, err error) {
	// Setup a default maximum stack depth so we don't error right away
	if e.MaxDepth <= 0 {
		e.MaxDepth = 500
	}

	// Setup the timeout channel if we have a timeout
	if e.Timeout > 0 {
		e.deadline = time.Now().UTC().Add(e.Timeout)
		e.timeoutCh = time.After(e.Timeout)
	}

	// If we have tracing enabled, initialize the trace structure
	if e.Trace != nil {
		*e.Trace = trace.Trace{}
		e.ruleMap = make(map[token.Pos]token.Pos)
	}

	// Evaluate. We do this in an inline closure so that we can catch
	// the bailout but continue to setup trace information.
	var obj object.Object
	func() {
		defer e.recoverBailout(&result, &err)
		obj = e.eval(e.File, e.Scope)
	}()

	// If we have tracing enabled, then finalize all the tracing output
	if e.Trace != nil {
		rules := make(map[string]*trace.Rule, len(e.traces))
		for _, t := range e.traces {
			rules[t.Ident] = t
		}

		e.Trace.Rules = rules
	}

	// If an error occurred, return that
	if err != nil {
		return false, err
	}

	if obj == nil {
		// This ultimately means that no main was given to this policy.
		return false, &NoMainError{
			File:    e.File,
			FileSet: e.FileSet,
		}
	}

	// The result can be undefined, which is a special error type
	if undef, ok := obj.(*object.UndefinedObj); ok {
		return false, &UndefError{
			FileSet: e.FileSet,
			Pos:     undef.Pos,
		}
	}

	boolObj, ok := obj.(*object.BoolObj)
	if !ok {
		return false, fmt.Errorf("Top-level result must be boolean, got: %s", obj.Type())
	}

	return boolObj.Value, nil
}

// ----------------------------------------------------------------------------
// Errors

// A bailout panic is raised to indicate early termination.
type bailout struct{ Err error }

// recoverBailout is the deferred function that catches bailout
// errors and sets the proper result and error.
func (e *evalState) recoverBailout(result *bool, err *error) {
	if e := recover(); e != nil {
		// resume same panic if it's not a bailout
		bo, ok := e.(bailout)
		if !ok {
			panic(e)
		}

		// Bailout always results in policy failure
		*result = false
		*err = bo.Err
	}
}

// EvalError is the most common error type returned by Eval.
type EvalError struct {
	Message string
	Scope   *object.Scope
	FileSet *token.FileSet
	Pos     token.Pos
}

func (e *EvalError) Error() string {
	if e.FileSet == nil {
		return fmt.Sprintf("At unknown location: %s", e.Message)
	}

	pos := e.FileSet.Position(e.Pos)
	return fmt.Sprintf("%s: %s", pos, e.Message)
}

// UndefError is returned when the top-level result is undefined.
type UndefError struct {
	FileSet *token.FileSet // FileSet to look up positions
	Pos     []token.Pos    // Positions where undefined was created
}

func (e *UndefError) Error() string {
	if e.FileSet == nil {
		return fmt.Sprintf("undefined behavior at unknown location")
	}

	// Get all the positions
	locs := make([]string, len(e.Pos))
	for i, p := range e.Pos {
		locs[i] = e.FileSet.Position(p).String()
	}

	return fmt.Sprintf(errUndefined, strings.Join(locs, "\n"))
}

// NoMainError is returned when the top-level result is nil (so no main defined).
type NoMainError struct {
	File    *ast.File      // The file
	FileSet *token.FileSet // FileSet for the file
}

func (e *NoMainError) Error() string {
	if e.FileSet == nil {
		return fmt.Sprintf("no main at undefined location")
	}

	return fmt.Sprintf(errNoMain, e.FileSet.Position(e.File.Pos()))
}

// err is a shortcut to easily produce errors from evaluation.
func (e *evalState) err(msg string, n ast.Node, s *object.Scope) {
	panic(bailout{Err: &EvalError{
		Message: msg,
		FileSet: e.FileSet,
		Pos:     n.Pos(),
		Scope:   s,
	}})
}

// TimeoutError is returned when the execution times out.
type TimeoutError struct {
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf(errTimeout, e.Timeout)
}

// ----------------------------------------------------------------------------
// Timeouts

func (e *evalState) checkTimeout() {
	select {
	case <-e.timeoutCh:
		// Timeout!
		panic(bailout{Err: &TimeoutError{Timeout: e.Timeout}})

	default:
		// We're okay.
	}
}

// ----------------------------------------------------------------------------
// Evaluation

func (e *evalState) eval(raw ast.Node, s *object.Scope) object.Object {
	// Check the timeout at every point we evaluate. This is a really coarse
	// scope and we can increase performance by making this more fine-grained
	// to nodes that are actually slow.
	e.checkTimeout()

	switch n := raw.(type) {
	case nil:
		// Do nothing
		return nil

	// File
	case *ast.File:
		return e.evalFile(n, s)

	// Statements

	case *ast.AssignStmt:
		e.evalAssign(n, s)

	case *ast.BlockStmt:
		return e.evalBlockStmt(n, s)

	case *ast.IfStmt:
		return e.evalIfStmt(n, s)

	case *ast.CaseStmt:
		return e.evalCaseStmt(n, s)

	case *ast.ForStmt:
		return e.evalForStmt(n, s)

	case *ast.BranchStmt:
		switch n.Tok {
		case token.BREAK:
			e.returnObj = runtimeBreakObj
		case token.CONTINUE:
			e.returnObj = runtimeContinueObj
		default:
			e.err(fmt.Sprintf("unexpected BranchStmt token: %s", n.Tok), raw, s)
		}

		return nil

	case *ast.ReturnStmt:
		e.returnObj = e.eval(n.Result, s)
		return nil

	case *ast.ExprStmt:
		e.eval(n.X, s)
		e.returnObj = nil
		return nil

	// Expressions

	case *ast.Ident:
		return e.evalIdent(n, s, false)

	case *ast.BasicLit:
		return e.evalBasicLit(n, s)

	case *ast.ListLit:
		elts := make([]object.Object, len(n.Elts))
		for i, n := range n.Elts {
			elts[i] = e.eval(n, s)
		}

		return &object.ListObj{Elts: elts}

	case *ast.MapLit:
		elts := make([]object.KeyedObj, len(n.Elts))
		for i, n := range n.Elts {
			kv := n.(*ast.KeyValueExpr)
			elts[i] = object.KeyedObj{
				Key:   e.eval(kv.Key, s),
				Value: e.eval(kv.Value, s),
			}
		}

		return &object.MapObj{Elts: elts}

	case *ast.RuleLit:
		// Just set the rule literal, we don't evaluate
		return &object.RuleObj{Scope: s, Rule: n}

	case *ast.FuncLit:
		return &object.FuncObj{Params: n.Params, Body: n.Body.List, Scope: s}

	case *ast.UnaryExpr:
		return e.evalUnaryExpr(n, s)

	case *ast.BinaryExpr:
		return e.evalBinaryExpr(n, s)

	case *ast.IndexExpr:
		return e.evalIndexExpr(n, s)

	case *ast.SliceExpr:
		return e.evalSliceExpr(n, s)

	case *ast.QuantExpr:
		return e.evalQuantExpr(n, s)

	case *ast.CallExpr:
		return e.evalCallExpr(n, s)

	case *ast.SelectorExpr:
		return e.evalSelectorExpr(n, s)

	case *ast.ParenExpr:
		return e.eval(n.X, s)

	// Custom AST nodes

	case *astImportExpr:
		return e.evalImportExpr(n, s)

	default:
		e.err(fmt.Sprintf("unexpected AST type: %T", raw), raw, s)
	}

	return nil
}
func (e *evalState) evalIdent(n *ast.Ident, s *object.Scope, moduleOk bool) object.Object {
	if n.Name == UndefinedName {
		return &object.UndefinedObj{Pos: []token.Pos{n.Pos()}}
	}

	obj := s.Lookup(n.Name)
	if obj == nil {
		switch n.Name {
		case "error":
			return object.ExternalFunc(e.funcError)

		case "print":
			return object.ExternalFunc(e.funcPrint)

		default:
			if _, ok := e.imports[n.Name]; ok {
				e.err(fmt.Sprintf(
					"import %q cannot be accessed without a selector expression",
					n.Name), n, s)
			}

			e.err(fmt.Sprintf("unknown identifier accessed: %s", n.Name), n, s)
		}
	}

	switch o := obj.(type) {
	case *object.ModuleObj:
		if moduleOk {
			// moduleOk is generally set when looking up an ident in a
			// selector expression. Return the module in this case.
			return o
		}

		// Otherwise, we block modules like we block imports.
		e.err(fmt.Sprintf(
			"module %q cannot be accessed without a selector expression",
			n.Name), n, s)

	case *object.RuleObj:
		return e.evalRuleObj(n.Name, o)
	}

	return obj
}

func (e *evalState) evalAssign(n *ast.AssignStmt, s *object.Scope) {
	rhs := n.Rhs

	// If the token isn't a plain assignment token, assume it is an
	// operator token (+= , -=, etc.) and convert the RHS to a binary
	// expression that performs that operation.
	if n.Tok != token.ASSIGN {
		var op token.Token
		switch n.Tok {
		case token.ADD_ASSIGN:
			op = token.ADD

		case token.SUB_ASSIGN:
			op = token.SUB

		case token.MUL_ASSIGN:
			op = token.MUL

		case token.QUO_ASSIGN:
			op = token.QUO

		case token.REM_ASSIGN:
			op = token.REM

		default:
			e.err(fmt.Sprintf("unsupported assignment token: %s", n.Tok), n, s)
		}

		// Convert the RHS to a binary expression equivalent for the given
		// operation. For example: a += 2 turns into a = a + 2
		rhs = &ast.BinaryExpr{X: n.Lhs, Op: op, Y: rhs}
	}

	// Determine the LHS
	switch lhs := n.Lhs.(type) {
	case *ast.Ident:
		obj := e.eval(rhs, s)
		dstS := s.Scope(lhs.Name)
		dstS.Objects[lhs.Name] = obj

		// If we're tracing and this is a rule assignment, record the
		// assignment location so that we can use that.
		if e.Trace != nil {
			if r, ok := obj.(*object.RuleObj); ok {
				e.ruleMap[r.Rule.Pos()] = lhs.Pos()
			}
		}

	case *ast.IndexExpr:
		obj := e.eval(rhs, s)
		e.evalIndexAssign(lhs, s, obj)

		// If we're tracing and this is a rule assignment, record the
		// assignment location so that we can use that.
		if e.Trace != nil {
			if r, ok := obj.(*object.RuleObj); ok {
				e.ruleMap[r.Rule.Pos()] = lhs.Pos()
			}
		}

	default:
		e.err(
			fmt.Sprintf("unsupported left-hand side of assignment: %T", n.Lhs),
			n, s)
	}
}

func (e *evalState) evalStmtList(n []ast.Stmt, s *object.Scope) object.Object {
	for _, stmt := range n {
		e.eval(stmt, s)
		if e.returnObj != nil {
			return nil
		}
	}

	return nil

}

func (e *evalState) evalBlockStmt(n *ast.BlockStmt, s *object.Scope) object.Object {
	return e.evalStmtList(n.List, s)
}

func (e *evalState) evalIfStmt(n *ast.IfStmt, s *object.Scope) object.Object {
	// Evaluate condition
	x := e.eval(n.Cond, s)
	b, ok := x.(*object.BoolObj)
	if !ok {
		e.err(
			fmt.Sprintf("if condition must result in a boolean, got %s", x.Type()),
			n.Cond, s)
	}

	if b.Value {
		return e.evalBlockStmt(n.Body, s)
	}

	return e.eval(n.Else, s)
}

func (e *evalState) evalCaseStmt(n *ast.CaseStmt, s *object.Scope) object.Object {
	// Evaluate expression
	var x object.Object
	if n.Expr != nil {
		x = e.eval(n.Expr, s)
	} else {
		// if no expression was provided, we set our object to be true
		x = &object.BoolObj{Value: true}
	}

	for _, bs := range n.Block.List {
		stmt, ok := bs.(*ast.CaseWhenClause)
		if !ok {
			e.err(
				"case statement condition is invalid", bs, s)
		}
		// if we are on "else", eval it's statments
		if stmt.Expr == nil {
			return e.evalStmtList(stmt.List, s)
		}

		// get the expression
		for _, exp := range stmt.Expr {
			y := e.eval(exp, s)
			r := e.cmpObjects_eq(x, y)
			b, ok := r.(*object.BoolObj)
			if !ok {
				continue
			}
			if b == object.True {
				return e.evalStmtList(stmt.List, s)
			}
		}
	}

	return nil
}

func (e *evalState) evalForStmt(n *ast.ForStmt, s *object.Scope) object.Object {
	// Create a new scope
	s = e.enterFrame(n, s)
	defer e.exitFrame(n, s)

	// Loop over the elements
	var result object.Object
	e.eltLoop(n.Expr, s, n.Name, n.Name2, func(s *object.Scope) bool {
		result = e.eval(n.Body, s)

		// If a break statement was reached, then exit the for loop.
		if e.returnObj != nil {
			switch e.returnObj {
			case runtimeBreakObj:
				e.returnObj = nil
				return false

			case runtimeContinueObj:
				e.returnObj = nil
				return true

			default:
				// "return" has been called and the return object contains
				// real data. Don't continue, and don't reset the return
				// object. This will allow the return object to properly
				// cascade down all scopes.
				return false
			}
		}

		return result == nil
	})

	return result
}

func (e *evalState) evalBasicLit(n *ast.BasicLit, s *object.Scope) object.Object {
	switch n.Kind {
	case token.INT:
		v, err := strconv.ParseInt(n.Value, 0, 64)
		if err != nil {
			e.err(err.Error(), n, s)
		}

		return &object.IntObj{Value: v}

	case token.FLOAT:
		v, err := strconv.ParseFloat(n.Value, 64)
		if err != nil {
			e.err(err.Error(), n, s)
		}

		return &object.FloatObj{Value: v}

	case token.STRING:
		v, err := strconv.Unquote(n.Value)
		if err != nil {
			e.err(err.Error(), n, s)
		}

		return &object.StringObj{Value: v}

	default:
		e.err(fmt.Sprintf("unknown basic literal type: %s", n.Kind), n, s)
		return nil
	}
}

func (e *evalState) evalFile(n *ast.File, s *object.Scope) object.Object {
	e.imports = make(map[string]sdk.Import)

	// Find implicit imports in our scope
	for k, obj := range s.Objects {
		if r, ok := obj.(*object.RuntimeObj); ok {
			// Remove the runtime object
			delete(s.Objects, k)

			// If it is an import, then we store it directly
			impt, ok := r.Value.(sdk.Import)
			if !ok {
				e.err(fmt.Sprintf(
					"internal error: runtime object %q unknown type %T",
					k, obj), n, s)
			}

			e.imports[k] = impt
		}
	}

	// Load the imports
	for _, impt := range n.Imports {
		// This shouldn't be possible since the parser verifies it.
		if impt.Path.Kind != token.STRING {
			e.err(fmt.Sprintf(
				"internal error, import path is not a string: %#v", impt), impt, s)
		}

		path, err := strconv.Unquote(impt.Path.Value)
		if err != nil {
			e.err(err.Error(), impt, s)
		}

		// Is this an available module?
		if mod, ok := e.Modules[path]; ok {
			name := path
			if impt.Name != nil {
				name = impt.Name.Name
			}

			s.Objects[name] = e.evalModule(mod, impt, s)

			// The rest of the loop is for imports using the plugin API, so short
			// circuit here.
			continue
		}

		if e.Importer == nil {
			e.err(fmt.Sprintf("import %q not found", path), impt, s)
		}

		obj, err := e.Importer.Import(path)
		if err != nil {
			e.err(err.Error(), impt, s)
		}

		name := path
		if impt.Name != nil {
			name = impt.Name.Name
		}

		e.imports[name] = obj
	}

	// Execute the statements
	for _, stmt := range n.Stmts {
		e.eval(stmt, s)
	}

	// Look up the "main" entrypoint
	obj := s.Lookup("main")
	if obj == nil {
		return obj
	}

	// If the top-level object is a rule, then we evaluate it
	if r, ok := obj.(*object.RuleObj); ok {
		obj = e.evalRuleObj("main", r)
	}

	return obj
}

func (e *evalState) evalModule(m *Module, impt *ast.ImportSpec, s *object.Scope) *object.ModuleObj {
	ms := e.enterModule(impt, s)
	defer e.exitModule()

	// Eval the module
	e.eval(m.Compiled.file, ms)

	return &object.ModuleObj{
		Scope: ms,
	}
}

func (e *evalState) enterModule(impt *ast.ImportSpec, s *object.Scope) *object.Scope {
	path, err := strconv.Unquote(impt.Path.Value)
	if err != nil {
		e.err(fmt.Sprintf("malformed import %q on module entry", impt.Path.Value), impt, s)
	}

	if err := e.moduleStack.EnterModule(path); err != nil {
		e.err(err.Error(), impt, s)
	}

	return object.NewScope(Universe())
}

func (e *evalState) exitModule() {
	e.moduleStack.ExitModule()
}

func (e *evalState) evalIndexExpr(n *ast.IndexExpr, s *object.Scope) object.Object {
	// Get the lhs
	x := e.eval(n.X, s)
	if x == object.Null {
		return &object.UndefinedObj{Pos: []token.Pos{n.X.Pos()}}
	}

	// Evaluate the index
	idxRaw := e.eval(n.Index, s)

	switch v := x.(type) {
	case *object.ListObj:
		idx, ok := idxRaw.(*object.IntObj)
		if !ok {
			e.err(fmt.Sprintf("indexing a list requires an int key, got %s", idxRaw.Type()), n, s)
		}

		idxVal := idx.Value
		if idxVal >= int64(len(v.Elts)) {
			return &object.UndefinedObj{Pos: []token.Pos{n.X.Pos()}}
		}

		if idxVal < 0 {
			idxVal += int64(len(v.Elts))
		}

		// If we still have a negative index here, we are out of bounds. Return
		// undefined.
		if idxVal < 0 {
			return &object.UndefinedObj{Pos: []token.Pos{n.X.Pos()}}
		}

		return v.Elts[idxVal]

	case *object.MapObj:
		for _, elt := range v.Elts {
			// This is terrible.
			if elt.Key.Type() == idxRaw.Type() && elt.Key.String() == idxRaw.String() {
				return elt.Value
			}
		}

		return &object.UndefinedObj{Pos: []token.Pos{n.X.Pos()}}

	case *object.MemoizedRemoteObj:
		for _, elt := range v.Context.Elts {
			if elt.Key.Type() == idxRaw.Type() && elt.Key.String() == idxRaw.String() {
				if m, ok := elt.Value.(*object.MapObj); ok {
					// Ensure that the metadata on the outer MemoizedRemoteObj
					// follows the lookup. Increment depth to note that we have
					// traversed one level down.
					return &object.MemoizedRemoteObj{
						Tag:     v.Tag,
						Depth:   v.Depth + 1,
						Context: m,
					}
				}

				return elt.Value
			}
		}

		return &object.UndefinedObj{Pos: []token.Pos{n.X.Pos()}}

	default:
		e.err(fmt.Sprintf("only a list or map can be indexed, got %s", x.Type()), n, s)
		return nil
	}
}

func (e *evalState) evalIndexAssign(n *ast.IndexExpr, s *object.Scope, o object.Object) {
	// Get the lhs
	x := e.eval(n.X, s)
	if x == object.Null {
		// This should more than likely not happen - attempting to index a missing
		// identifier fails earlier up to the stack. Leaving this here to catch any
		// edge cases I don't know about yet.
		e.err("index assignment attempt on a null object", n, s)
	}

	// Evaluate the index
	idxRaw := e.eval(n.Index, s)

	switch v := x.(type) {
	case *object.ListObj:
		idx, ok := idxRaw.(*object.IntObj)
		if !ok {
			e.err(fmt.Sprintf("indexing a list requires an int key, got %s", idxRaw.Type()), n, s)
		}

		idxVal := idx.Value
		if idxVal >= int64(len(v.Elts)) {
			e.err(fmt.Sprintf("index out of range on assignment (%d >= %d)", idxVal, len(v.Elts)), n, s)
		}

		if idxVal < 0 {
			idxVal += int64(len(v.Elts))
		}

		// If we still have a negative index here, we are out of bounds - return a
		// runtime error.
		if idxVal < 0 {
			e.err(fmt.Sprintf(
				"negative index out of range on assignment (value of %d cannot be lower than length of list (%d) * -1)",
				idx.Value, len(v.Elts),
			), n, s)
		}

		v.Elts[idxVal] = o

	case *object.MapObj:
		for i, elt := range v.Elts {
			if elt.Key.Type() == idxRaw.Type() && elt.Key.String() == idxRaw.String() {
				v.Elts[i].Value = o
				// Done here, short circuit so that we don't re-assign below
				return
			}
		}

		v.Elts = append(v.Elts, object.KeyedObj{
			Key:   idxRaw,
			Value: o,
		})

	case *object.MemoizedRemoteObj:
		for i, elt := range v.Context.Elts {
			if elt.Key.Type() == idxRaw.Type() && elt.Key.String() == idxRaw.String() {
				v.Context.Elts[i].Value = o
				// Done here, short circuit so that we don't re-assign below
				return
			}
		}

		v.Context.Elts = append(v.Context.Elts, object.KeyedObj{
			Key:   idxRaw,
			Value: o,
		})

	default:
		e.err(fmt.Sprintf("index assignment only valid for lists or maps, got %s", x.Type()), n, s)
	}

	return
}

func (e *evalState) evalSliceExpr(n *ast.SliceExpr, s *object.Scope) object.Object {
	// Eval the LHS first
	xRaw := e.eval(n.X, s)

	// Setup the low/high for slicing
	var low, high int

	// Get the low value if it is set
	if n.Low != nil {
		raw := e.eval(n.Low, s)
		v, ok := raw.(*object.IntObj)
		if !ok {
			e.err(fmt.Sprintf("slice index must be an int, got %s", raw.Type()), n.Low, s)
		}

		low = int(v.Value)
	}

	// Get the high value if it is set
	if n.High != nil {
		raw := e.eval(n.High, s)
		v, ok := raw.(*object.IntObj)
		if !ok {
			e.err(fmt.Sprintf("slice index must be an int, got %s", raw.Type()), n.Low, s)
		}

		high = int(v.Value)
	}

	switch x := xRaw.(type) {
	case *object.ListObj:
		if n.High == nil {
			high = len(x.Elts)
		}

		if low > high {
			return &object.UndefinedObj{Pos: []token.Pos{n.Pos()}}
		}

		if low < 0 || low > len(x.Elts) {
			return &object.UndefinedObj{Pos: []token.Pos{n.Pos()}}
		}

		if high < 0 || high > len(x.Elts) {
			return &object.UndefinedObj{Pos: []token.Pos{n.Pos()}}
		}

		return &object.ListObj{Elts: x.Elts[low:high]}

	case *object.StringObj:
		if n.High == nil {
			high = len(x.Value)
		}

		if low > high {
			return &object.UndefinedObj{Pos: []token.Pos{n.Pos()}}
		}

		if low < 0 || low > len(x.Value) {
			return &object.UndefinedObj{Pos: []token.Pos{n.Pos()}}
		}

		if high < 0 || high > len(x.Value) {
			return &object.UndefinedObj{Pos: []token.Pos{n.Pos()}}
		}

		return &object.StringObj{Value: x.Value[low:high]}

	case *object.UndefinedObj:
		return x

	default:
		if xRaw == object.Null {
			return &object.UndefinedObj{Pos: []token.Pos{n.Pos()}}
		}

		e.err(fmt.Sprintf("only lists and strings can be sliced, got %s", xRaw.Type()), n, s)
		panic("unreachable")
	}
}

func (e *evalState) evalQuantExpr(n *ast.QuantExpr, s *object.Scope) object.Object {
	// Create a new scope
	s = e.enterFrame(n, s)
	defer e.exitFrame(n, s)

	// Loop over the elements
	var result object.Object
	e.eltLoop(n.Expr, s, n.Name, n.Name2, func(s *object.Scope) bool {
		// Evaluate the body
		x := e.eval(n.Body, s)

		b, ok := x.(*object.BoolObj)
		if !ok {
			// If it is undefined, it is undefined
			if _, ok := x.(*object.UndefinedObj); ok {
				result = x
				return true
			}

			e.err(fmt.Sprintf("body of quantifier expression must result in bool"), n.Body, s)
		}

		switch {
		case n.Op == token.ANY && b.Value:
			result = object.True
			return false

		case n.Op == token.ALL && !b.Value:
			result = object.False
			return false
		}

		return true
	})

	if result == nil {
		result = object.Bool(n.Op == token.ALL)
	}

	return result
}

func (e *evalState) eltLoop(n ast.Expr, s *object.Scope, n1, n2 *ast.Ident, f func(*object.Scope) bool) {
	raw := e.eval(n, s)
	switch x := raw.(type) {
	case *object.ListObj:
		idx := n1
		value := n2
		if n2 == nil {
			idx = nil
			value = n1
		}

		for i, elt := range x.Elts {
			if idx != nil {
				s.Objects[idx.Name] = &object.IntObj{Value: int64(i)}
			}
			if value != nil {
				s.Objects[value.Name] = elt
			}

			cont := f(s)
			if !cont {
				break
			}
		}

	case *object.MapObj:
		key := n1
		value := n2
		if n2 == nil {
			key = n1
			value = nil
		}

		for _, elt := range x.Elts {
			if key != nil {
				s.Objects[key.Name] = elt.Key
			}
			if value != nil {
				s.Objects[value.Name] = elt.Value
			}

			cont := f(s)
			if !cont {
				break
			}
		}

	default:
		e.err(fmt.Sprintf("unsupported type for looping: %s", raw.Type()), n, s)
	}
}

func (e *evalState) evalUnaryExpr(n *ast.UnaryExpr, s *object.Scope) object.Object {
	switch n.Op {
	case token.NOT, token.NOTSTR:
		// Evaluate the field
		x := e.eval(n.X, s)

		// If it is undefined, it is undefined
		if _, ok := x.(*object.UndefinedObj); ok {
			return x
		}

		bx, ok := x.(*object.BoolObj)
		if !ok {
			e.err(fmt.Sprintf("operand needs to be boolean, got %s", x.Type()), n, s)
		}

		// Invert and return
		return object.Bool(!bx.Value)

	case token.SUB:
		// Evaluate the field
		x := e.eval(n.X, s)

		// If it is undefined, it is undefined
		if _, ok := x.(*object.UndefinedObj); ok {
			return x
		}

		// It must be an int
		switch v := x.(type) {
		case *object.UndefinedObj:
			return v

		case *object.IntObj:
			return &object.IntObj{Value: -v.Value}

		case *object.FloatObj:
			return &object.FloatObj{Value: -v.Value}

		default:
			e.err(fmt.Sprintf("unary negation requires int or float, got %s", v.Type()), n, s)
			return nil
		}
	default:
		e.err(fmt.Sprintf("unsupported operator: %s", n.Op), n, s)
		return nil
	}
}

func (e *evalState) evalBinaryExpr(n *ast.BinaryExpr, s *object.Scope) object.Object {
	switch n.Op {
	case token.LAND, token.LOR, token.LXOR:
		return e.evalBooleanExpr(n, s)

	case token.EQL, token.NEQ, token.LSS, token.GTR, token.LEQ, token.GEQ,
		token.IS, token.ISNOT:
		return e.evalRelExpr(n, s)

	case token.ADD, token.SUB, token.MUL, token.QUO, token.REM:
		return e.evalMathExpr(n, s)

	case token.CONTAINS:
		return e.evalBinaryNeg(n, e.evalContainsExpr(n, s))

	case token.IN:
		return e.evalBinaryNeg(n, e.evalInExpr(n, s))

	case token.MATCHES:
		return e.evalBinaryNeg(n, e.evalMatchesExpr(n, s))

	case token.ELSE:
		return e.evalElseExpr(n, s)

	default:
		e.err(fmt.Sprintf("unsupported operator: %s", n.Op), n, s)
		return nil
	}
}

func (e *evalState) evalBinaryNeg(n *ast.BinaryExpr, obj object.Object) object.Object {
	if !n.OpNeg {
		return obj
	}

	switch x := obj.(type) {
	case *object.BoolObj:
		return object.Bool(!x.Value)

	default:
		// Invalid types are handled elsewhere
		return obj
	}
}

func (e *evalState) evalBooleanExpr(n *ast.BinaryExpr, s *object.Scope) object.Object {
	// Eval left operand
	e.pushTrace(n.X)
	x := e.popTrace(e.eval(n.X, s))

	// If x is undefined, switch to undefined logic
	if _, ok := x.(*object.UndefinedObj); ok {
		// If we're in an AND, this is failing no matter what so we don't
		// evaluate y.
		var y object.Object
		if n.Op != token.LAND {
			e.pushTrace(n.Y)
			y = e.popTrace(e.eval(n.Y, s))
		}

		return e.evalBooleanExpr_undef(n, s, x, y)
	}

	// Verify X is a boolean
	bx, ok := x.(*object.BoolObj)
	if !ok {
		e.err(fmt.Sprintf("left operand needs to be boolean, got %s", x.Type()), n, s)
	}

	// Short circuit-logic
	if n.Op == token.LAND && !bx.Value {
		return object.False
	} else if n.Op == token.LOR && bx.Value {
		return object.True
	}

	// Eval right operand
	e.pushTrace(n.Y)
	y := e.popTrace(e.eval(n.Y, s))

	// If y is undefined it is always undefined
	if _, ok := y.(*object.UndefinedObj); ok {
		return e.evalBooleanExpr_undef(n, s, y, x)
	}

	by, ok := y.(*object.BoolObj)
	if !ok {
		e.err(fmt.Sprintf("right operand needs to be boolean, got %s", y.Type()), n, s)
	}

	// Perform the actual logic
	switch n.Op {
	case token.LAND:
		return object.Bool(bx.Value && by.Value)

	case token.LOR:
		return object.Bool(bx.Value || by.Value)

	default:
		e.err(fmt.Sprintf("unsupported operator: %s", n.Op), n, s)
		return nil
	}
}

func (e *evalState) evalBooleanExpr_undef(
	n *ast.BinaryExpr, s *object.Scope,
	undef object.Object, y object.Object) object.Object {
	// If it is not OR, its always going to be undefined
	if n.Op != token.LOR {
		return undef
	}

	// If y is true then we escape
	if b, ok := y.(*object.BoolObj); ok && b.Value {
		return b
	}

	// Otherwise, undefined
	return undef
}

func (e *evalState) evalMathExpr(n *ast.BinaryExpr, s *object.Scope) object.Object {
	// Eval left operand
	x := e.eval(n.X, s)
	if _, ok := x.(*object.UndefinedObj); ok {
		return x
	}

	// Eval right operand
	y := e.eval(n.Y, s)
	if _, ok := y.(*object.UndefinedObj); ok {
		return y
	}

	xtyp := x.Type()
	ytyp := y.Type()
	if xtyp != ytyp {
		// Non-matching type. Check to see if it's a supported numeric
		// operation. Otherwise, it's an error.
		switch {
		case xtyp == object.FLOAT && ytyp == object.INT:
			return e.evalMathExpr_float(
				n, s,
				x.(*object.FloatObj).Value,
				float64(y.(*object.IntObj).Value),
			)

		case xtyp == object.INT && ytyp == object.FLOAT:
			return e.evalMathExpr_float(
				n, s,
				float64(x.(*object.IntObj).Value),
				y.(*object.FloatObj).Value,
			)

		default:
			e.err(fmt.Sprintf(
				"math: non-number operation on different types (%s/%s)",
				xtyp, ytyp),
				n, s)
		}
	}

	switch xObj := x.(type) {
	case *object.IntObj:
		return e.evalMathExpr_int(n, s, xObj.Value, y.(*object.IntObj).Value)

	case *object.FloatObj:
		return e.evalMathExpr_float(n, s, xObj.Value, y.(*object.FloatObj).Value)

	case *object.ListObj:
		return e.evalMathExpr_list(n, s, xObj, y.(*object.ListObj))

	case *object.StringObj:
		return e.evalMathExpr_string(n, s, xObj, y.(*object.StringObj))

	default:
		e.err(fmt.Sprintf("can't perform math on type %s", x.Type()), n, s)
		return nil
	}
}

func (e *evalState) evalMathExpr_int(n *ast.BinaryExpr, s *object.Scope, x, y int64) object.Object {
	var result int64
	switch n.Op {
	case token.ADD:
		result = x + y

	case token.SUB:
		result = x - y

	case token.MUL:
		result = x * y

	case token.QUO:
		if y == 0 {
			e.err("division by zero", n, s)
			return nil
		}

		result = x / y

	case token.REM:
		result = x % y

	default:
		e.err(fmt.Sprintf("unsupported operator: %s", n.Op), n, s)
		return nil
	}

	return &object.IntObj{Value: result}
}

func (e *evalState) evalMathExpr_float(n *ast.BinaryExpr, s *object.Scope, x, y float64) object.Object {
	var result float64
	switch n.Op {
	case token.ADD:
		result = x + y

	case token.SUB:
		result = x - y

	case token.MUL:
		result = x * y

	case token.QUO:
		if y == 0 {
			e.err("division by zero", n, s)
			return nil
		}

		result = x / y

	case token.REM:
		result = math.Mod(x, y)

	default:
		e.err(fmt.Sprintf("unsupported operator: %s", n.Op), n, s)
		return nil
	}

	return &object.FloatObj{Value: result}
}

func (e *evalState) evalMathExpr_list(n *ast.BinaryExpr, s *object.Scope, x, y *object.ListObj) object.Object {
	switch n.Op {
	case token.ADD:
		xLen := len(x.Elts)
		elts := make([]object.Object, xLen+len(y.Elts))
		copy(elts, x.Elts)
		copy(elts[xLen:], y.Elts)
		return &object.ListObj{Elts: elts}

	default:
		e.err(fmt.Sprintf("unsupported operator for lists: %s", n.Op), n, s)
		return nil
	}
}

func (e *evalState) evalMathExpr_string(n *ast.BinaryExpr, s *object.Scope, x, y *object.StringObj) object.Object {
	switch n.Op {
	case token.ADD:
		return &object.StringObj{Value: x.Value + y.Value}

	default:
		e.err(fmt.Sprintf("unsupported operator for strings: %s", n.Op), n, s)
		return nil
	}
}

func (e *evalState) evalRelExpr(n *ast.BinaryExpr, s *object.Scope) object.Object {
	// Eval left and right operand
	x := e.eval(n.X, s)
	y := e.eval(n.Y, s)

	switch n.Op {
	case token.EQL, token.IS:
		return e.evalRelExpr_eq(n, s, x, y)

	case token.NEQ, token.ISNOT:
		result := e.evalRelExpr_eq(n, s, x, y)
		if _, ok := result.(*object.UndefinedObj); ok {
			return result
		}

		if result.(*object.BoolObj) == object.False {
			return object.True
		}

		return object.False
	}

	// Remaining operators should all be ordered
	return e.evalRelExpr_order(n, s, x, y)
}

func (e *evalState) cmpObjects_eq(x, y object.Object) object.Object {
	// Undefined short-circuiting
	if _, ok := x.(*object.UndefinedObj); ok {
		return x
	}
	if _, ok := y.(*object.UndefinedObj); ok {
		return y
	}

	// Null check
	if x == object.Null && y == object.Null {
		return object.True
	}

	switch xObj := x.(type) {
	case *object.BoolObj:
		if yObj, ok := y.(*object.BoolObj); ok {
			return object.Bool(xObj.Value == yObj.Value)
		}

	case *object.IntObj:
		switch yObj := y.(type) {
		case *object.IntObj:
			return object.Bool(xObj.Value == yObj.Value)

		case *object.FloatObj:
			return object.Bool(float64(xObj.Value) == yObj.Value)
		}

	case *object.FloatObj:
		switch yObj := y.(type) {
		case *object.FloatObj:
			return object.Bool(xObj.Value == yObj.Value)

		case *object.IntObj:
			return object.Bool(xObj.Value == float64(yObj.Value))
		}

	case *object.StringObj:
		if yObj, ok := y.(*object.StringObj); ok {
			return object.Bool(xObj.Value == yObj.Value)
		}

	case *object.ListObj:
		if yObj, ok := y.(*object.ListObj); ok {
			if len(xObj.Elts) != len(yObj.Elts) {
				return object.False
			}

			for i := 0; i < len(xObj.Elts); i++ {
				switch result := e.cmpObjects_eq(xObj.Elts[i], yObj.Elts[i]).(type) {
				case *object.BoolObj:
					if result == object.False {
						return result
					}

				case *object.UndefinedObj:
					return result
				}
			}

			return object.True
		}
	}

	return object.False

}

func (e *evalState) evalRelExpr_eq(n *ast.BinaryExpr, s *object.Scope, x, y object.Object) object.Object {
	return e.cmpObjects_eq(x, y)
}

func (e *evalState) evalRelExpr_order(n *ast.BinaryExpr, s *object.Scope, x, y object.Object) object.Object {
	// Undefined short-circuiting
	if _, ok := x.(*object.UndefinedObj); ok {
		return x
	}
	if _, ok := y.(*object.UndefinedObj); ok {
		return y
	}

	switch xObj := x.(type) {
	case *object.IntObj:
		switch yObj := y.(type) {
		case *object.IntObj:
			return e.evalRelExpr_order_int(n, s, xObj.Value, yObj.Value)

		case *object.FloatObj:
			return e.evalRelExpr_order_int(n, s, int64(xObj.Value), int64(yObj.Value))
		}

	case *object.FloatObj:
		switch yObj := y.(type) {
		case *object.FloatObj:
			return e.evalRelExpr_order_float(n, s, xObj.Value, yObj.Value)

		case *object.IntObj:
			return e.evalRelExpr_order_float(n, s, float64(xObj.Value), float64(yObj.Value))
		}

	case *object.StringObj:
		if yObj, ok := y.(*object.StringObj); ok {
			return e.evalRelExpr_order_string(n, s, xObj.Value, yObj.Value)
		}
	}

	e.err(fmt.Sprintf(
		"ordered comparison not supported between types %s and %s",
		x.Type(), y.Type()),
		n, s)
	return nil
}

func (e *evalState) evalRelExpr_order_int(n *ast.BinaryExpr, s *object.Scope, x, y int64) object.Object {
	var result bool
	switch n.Op {
	case token.LSS:
		result = x < y

	case token.GTR:
		result = x > y

	case token.LEQ:
		result = x <= y

	case token.GEQ:
		result = x >= y

	default:
		e.err(fmt.Sprintf("unsupported operator: %s", n.Op), n, s)
		return nil
	}

	return object.Bool(result)
}

func (e *evalState) evalRelExpr_order_float(n *ast.BinaryExpr, s *object.Scope, x, y float64) object.Object {
	var result bool
	switch n.Op {
	case token.LSS:
		result = x < y

	case token.GTR:
		result = x > y

	case token.LEQ:
		result = x <= y

	case token.GEQ:
		result = x >= y

	default:
		e.err(fmt.Sprintf("unsupported operator: %s", n.Op), n, s)
		return nil
	}

	return object.Bool(result)
}

func (e *evalState) evalRelExpr_order_string(n *ast.BinaryExpr, s *object.Scope, x, y string) object.Object {
	var result bool
	switch n.Op {
	case token.LSS:
		result = x < y

	case token.GTR:
		result = x > y

	case token.LEQ:
		result = x <= y

	case token.GEQ:
		result = x >= y

	default:
		e.err(fmt.Sprintf("unsupported operator: %s", n.Op), n, s)
		return nil
	}

	return object.Bool(result)
}

func (e *evalState) evalContainsExpr(n *ast.BinaryExpr, s *object.Scope) object.Object {
	// Evaluate the left side
	x := e.eval(n.X, s)

	switch xv := x.(type) {
	case *object.ListObj:
		y := e.eval(n.Y, s)
		return e.evalContainsExpr_list(n, s, xv, y)

	case *object.MapObj:
		y := e.eval(n.Y, s)
		return e.evalContainsExpr_map(n, s, xv, y)

	case *object.MemoizedRemoteObj:
		y := e.eval(n.Y, s)
		return e.evalContainsExpr_map(n, s, xv.Context, y)

	case *object.StringObj:
		y := e.eval(n.Y, s)
		return e.evalContainsExpr_string(n, s, xv, y)

	default:
		e.err(fmt.Sprintf("left operand for contains must be list, map, or string got %s", x.Type()), n, s)
		return nil
	}
}

func (e *evalState) evalContainsExpr_list(
	n *ast.BinaryExpr, s *object.Scope, x *object.ListObj, y object.Object) object.Object {
	for _, elt := range x.Elts {
		switch result := e.evalRelExpr_eq(n, s, elt, y).(type) {
		case *object.BoolObj:
			if result == object.True {
				return result
			}

		case *object.UndefinedObj:
			return result
		}
	}

	return object.False
}

func (e *evalState) evalContainsExpr_map(
	n *ast.BinaryExpr, s *object.Scope, x *object.MapObj, y object.Object) object.Object {
	for _, kv := range x.Elts {
		elt := kv.Key
		switch result := e.evalRelExpr_eq(n, s, elt, y).(type) {
		case *object.BoolObj:
			if result == object.True {
				return result
			}

		case *object.UndefinedObj:
			return result
		}
	}

	return object.False
}

func (e *evalState) evalContainsExpr_string(
	n *ast.BinaryExpr, s *object.Scope, x *object.StringObj, y object.Object) object.Object {
	yv, ok := y.(*object.StringObj)
	if !ok {
		e.err(fmt.Sprintf(
			"both operands for contains/in must be a string for strings, got %q",
			y.Type()), n, s)
	}

	return object.Bool(strings.Contains(x.Value, yv.Value))
}

func (e *evalState) evalInExpr(n *ast.BinaryExpr, s *object.Scope) object.Object {
	// Evaluate the left side
	x := e.eval(n.X, s)
	if x.Type() == object.UNDEFINED {
		return x
	}

	// Evaluate the right side
	y := e.eval(n.Y, s)
	if y.Type() == object.UNDEFINED {
		return y
	}

	switch yv := y.(type) {
	case *object.ListObj:
		return e.evalContainsExpr_list(n, s, yv, x)

	case *object.MapObj:
		return e.evalContainsExpr_map(n, s, yv, x)

	case *object.MemoizedRemoteObj:
		return e.evalContainsExpr_map(n, s, yv.Context, x)

	case *object.StringObj:
		return e.evalContainsExpr_string(n, s, yv, x)

	default:
		e.err(fmt.Sprintf("right operand for in must be list, map, or string, got %s", y.Type()), n.Y, s)
		return nil
	}
}

func (e *evalState) evalMatchesExpr(n *ast.BinaryExpr, s *object.Scope) object.Object {
	// Evaluate the left side
	raw := e.eval(n.X, s)
	x, ok := raw.(*object.StringObj)
	if !ok {
		// If it is undefined, return it
		if _, ok := raw.(*object.UndefinedObj); ok {
			return raw
		}

		e.err(fmt.Sprintf("left operand for matches must be a string, got %s", raw.Type()), n, s)
	}

	// Evaluate the right
	raw = e.eval(n.Y, s)
	y, ok := raw.(*object.StringObj)
	if !ok {
		// If it is undefined, return it
		if _, ok := raw.(*object.UndefinedObj); ok {
			return raw
		}

		e.err(fmt.Sprintf("right operand for matches must be a string, got %s", raw.Type()), n, s)
	}

	// Parse the regular expression
	re, err := regexp.Compile(y.Value)
	if err != nil {
		e.err(fmt.Sprintf("invalid regular expression: %s", err), n.Y, s)
	}

	return object.Bool(re.MatchString(x.Value))
}

func (e *evalState) evalElseExpr(n *ast.BinaryExpr, s *object.Scope) object.Object {
	// Evaluate the left side
	raw := e.eval(n.X, s)
	if _, ok := raw.(*object.UndefinedObj); !ok {
		// If it isn't undefined, return it
		return raw
	}

	// Evaluate and return the right
	return e.eval(n.Y, s)
}

func (e *evalState) evalCallExpr(n *ast.CallExpr, s *object.Scope) object.Object {
	// Get the function
	raw := e.eval(n.Fun, s)
	switch raw.(type) {
	case *object.FuncObj:
	case *object.ExternalObj:
	case *object.MemoizedRemoteObjMiss:
		// Must be dispatched to the import
		return raw

	default:
		e.err(fmt.Sprintf("attempting to call non-function: %s", raw.Type()), n.Fun, s)
	}

	f, ok := raw.(*object.FuncObj)
	if !ok {
		// It has to be an external from above check
		ext, ok := raw.(*object.ExternalObj)
		if !ok {
			e.err(fmt.Sprintf("attempting to call non-function: %s", raw.Type()), n.Fun, s)
		}

		// Build the args
		args := make([]object.Object, len(n.Args))
		for i, arg := range n.Args {
			args[i] = e.eval(arg, s)
		}

		// Call it
		result, err := ext.External.Call(args)
		if err != nil {
			e.err(err.Error(), n.Fun, s)
		}

		resultObj, err := encoding.GoToObject(result)
		if err != nil {
			e.err(err.Error(), n.Fun, s)
		}

		if u, ok := resultObj.(*object.UndefinedObj); ok && u.Pos == nil {
			u.Pos = []token.Pos{n.Fun.Pos()}
		}

		return resultObj
	}

	// Check argument length
	if f != nil && len(f.Params) != len(n.Args) {
		e.err(fmt.Sprintf(
			"invalid number of arguments, expected %d, got %d",
			len(f.Params), len(n.Args)), n.Fun, s)
	}

	// Create a new scope for the function
	evalScope := e.enterFrame(n, f.Scope)
	defer e.exitFrame(n, evalScope)

	// Evaluate and set all the arguments
	for i, arg := range n.Args {
		evalScope.Objects[f.Params[i].Name] = e.eval(arg, s)
	}

	// Evaluate all the statements
	for _, stmt := range f.Body {
		e.eval(stmt, evalScope)
		if e.returnObj != nil {
			result := e.returnObj
			e.returnObj = nil
			return result
		}
	}

	// This shouldn't happen because semantic checks should catch this.
	e.err(errNoReturn, n, s)
	return nil
}

func (e *evalState) evalSelectorExpr(n *ast.SelectorExpr, s *object.Scope) object.Object {
	var raw object.Object
	if x, ok := n.X.(*ast.Ident); ok {
		raw = e.evalIdent(x, s, true)
	} else {
		raw = e.eval(n.X, s)
	}

	switch x := raw.(type) {
	case *object.ExternalObj:
		result, err := x.External.Get(n.Sel.Name)
		if err != nil {
			e.err(err.Error(), n.Sel, s)
		}
		if result == nil {
			return &object.UndefinedObj{Pos: []token.Pos{n.Sel.Pos()}}
		}

		obj, err := encoding.GoToObject(result)
		if err != nil {
			e.err(err.Error(), n.Sel, s)
		}

		return obj
	case *object.MapObj:
		for _, elt := range x.Elts {
			if s, ok := elt.Key.(*object.StringObj); ok && s.Value == n.Sel.Name {
				return elt.Value
			}
		}

		return &object.UndefinedObj{Pos: []token.Pos{n.Sel.Pos()}}

	case *object.MemoizedRemoteObj:
		for _, elt := range x.Context.Elts {
			if s, ok := elt.Key.(*object.StringObj); ok && s.Value == n.Sel.Name {
				if m, ok := elt.Value.(*object.MapObj); ok {
					// Ensure that the metadata on the outer MemoizedRemoteObj
					// follows the lookup. Increment depth to note that we have
					// traversed one level down.
					return &object.MemoizedRemoteObj{
						Tag:     x.Tag,
						Depth:   x.Depth + 1,
						Context: m,
					}
				}

				return elt.Value
			}
		}

		// Unknown selector lookups on MemoizedRemoteObj values are not
		// necessarily undefined. Return as a MemoizedRemoteObjMiss so that we
		// can try and look up the value through the plugin.
		return &object.MemoizedRemoteObjMiss{MemoizedRemoteObj: x}

	case *object.MemoizedRemoteObjMiss:
		// Pass this through, so it can be executed by the import.
		return x

	case *object.ModuleObj:
		if v := x.Scope.Lookup(n.Sel.Name); v != nil {
			// FIXME: Maybe? I think there might be a better way to do this, but I
			// can't think of one at this point in time. If we have a rule object in
			// a module, it has already gone through the rule literal eval stage and
			// this should technically be an eval on the rule.
			if r, ok := v.(*object.RuleObj); ok {
				return e.evalRuleObj(n.Sel.Name, r)
			}

			return v
		}

		return &object.UndefinedObj{Pos: []token.Pos{n.Sel.Pos()}}

	case *object.UndefinedObj:
		return x

	default:
		e.err(fmt.Sprintf(
			"selectors only available for imports, maps, and modules, got %s",
			raw.Type()), n, s)
		return nil
	}
}

func (e *evalState) evalImportExpr(n *astImportExpr, s *object.Scope) object.Object {
	// Lookup the import. If it is shadowed, then fall back.
	//
	// If the fallback results in a missed lookup on memoization, then
	// we populate that into the request Context so that it can be
	// sent along with the request.
	imptName := n.Import
	var reqCtx *object.MemoizedRemoteObj
	if v := s.Lookup(imptName); v != nil {
		result := e.eval(n.Original, s)
		if o, ok := result.(*object.MemoizedRemoteObjMiss); ok {
			imptName = o.Tag
			reqCtx = o.MemoizedRemoteObj
		} else {
			return result
		}
	}

	// Not an import, find the import in our map and call it
	impt, ok := e.imports[imptName]
	if !ok {
		e.err(fmt.Sprintf("import not found: %s", imptName), n, s)
	}

	// Start building the request
	req := &sdk.GetReq{
		ExecId:       e.ExecId,
		ExecDeadline: e.deadline,
		KeyId:        42,
	}

	// Process the keys. This includes skipping any that have may have
	// been traversed already on an import receiver.
	req.Keys = make([]sdk.GetKey, 0, len(n.Keys))
	for i, ixKey := range n.Keys {
		if reqCtx != nil && int64(i) < reqCtx.Depth {
			// Skip any keys that have been traversed already
			continue
		}

		key := sdk.GetKey{Key: ixKey.Key}
		// Translate the args out from each key. This must be nil (not just
		// an empty slice) if we aren't making a call (n.Args == nil).
		if ixKey.Args != nil {
			key.Args = make([]interface{}, len(ixKey.Args))
			for j, ixArg := range ixKey.Args {
				value, err := encoding.ObjectToGo(e.eval(ixArg, s), nil)
				if err != nil {
					e.err(fmt.Sprintf("error converting argument: %s", err), n, s)
				}

				key.Args[j] = value
			}
		}

		req.Keys = append(req.Keys, key)
	}

	// If we have a request context (receiver), convert the context.
	if reqCtx != nil {
		// Check to ensure that the context is an entirely string-keyed
		// map first.
		//
		// FIXME: This is expensive. We already do the traversal in the
		// subsequent ObjectToGo call, but the type enforcement supplied
		// will attempt to weakly encode values that don't type-match,
		// ie: ints, to strings if possible. It would be better if we
		// could control this behavior somehow, so that weak conversion
		// could be turned off, and the errors could happen in
		// ObjectToGo.
		for _, elt := range reqCtx.Context.Elts {
			if elt.Key.Type() != object.STRING {
				e.err("error converting object context for request: context has non-string map keys", n, s)
			}
		}

		// Ensure we supply a type to ObjectToGo (mapStringInterfaceTyp)
		// so that maps of an exclusive value type do not get converted
		// to that type, ie: a map[string]string for a map that has all
		// string values.
		reqCtxM, err := encoding.ObjectToGo(reqCtx.Context, mapStringInterfaceTyp)
		if err != nil {
			e.err(fmt.Sprintf("error converting object context for request: %s", err), n, s)
		}

		req.Context = reqCtxM.(map[string]interface{})
	}

	// Perform the external call
	results, err := impt.Get([]*sdk.GetReq{req})
	if err != nil {
		e.err(err.Error(), n, s)
	}

	// Find our resulting value
	var result *sdk.GetResult
	for _, v := range results {
		if v.KeyId == 42 {
			result = v
			break
		}
	}
	if result == nil {
		return &object.UndefinedObj{Pos: []token.Pos{n.Pos()}}
	}

	// Check for a returned context. If we have one, overwrite the
	// current context that we are working with.
	if result.Context != nil && reqCtx != nil {
		resCtxM, err := encoding.GoToObject(result.Context)
		if err != nil {
			e.err(fmt.Sprintf("error converting object context from response: %s", err), n, s)
		}

		// GoToObject always returns MapObj types for maps, so this
		// should be safe to assert here.
		reqCtx.Context = resCtxM.(*object.MapObj)
	}

	if result.Value == sdk.Undefined {
		return &object.UndefinedObj{Pos: []token.Pos{n.Pos()}}
	}

	obj, err := encoding.GoToObject(result.Value)
	if err != nil {
		e.err(err.Error(), n, s)
	}

	if m, ok := obj.(*object.MapObj); ok && result.Callable {
		// Result is callable, wrap in MemoizedRemoteObj so that we can
		// track it.
		return &object.MemoizedRemoteObj{
			Tag:     imptName,
			Context: m,
		}
	}

	return obj
}

func (e *evalState) evalRuleObj(ident string, r *object.RuleObj) object.Object {
	if r.Value != nil {
		return r.Value
	}

	// If this rule has a when predicate, check that.
	if r.Rule.When != nil {
		// If we haven't evalutated the when expression before, do it
		if r.WhenValue == nil {
			r.WhenValue = e.eval(r.Rule.When, r.Scope)
		}

		switch v := r.WhenValue.(type) {
		case *object.UndefinedObj:
			// If the predicate is undefined, we continue to chain it
			return r.WhenValue

		case *object.BoolObj:
			// If the predicate failed, then the result of the rule is always true
			if !v.Value {
				r.Value = object.True // Set the memoized value so we don't exec again
				return r.Value
			}

		default:
			e.err("rule predicate evaluated to a non-boolean value", r.Rule.When, r.Scope)
		}
	}

	// If tracing is enabled and we haven't traced the execution
	// of this rule yet, then we trace the execution of this rule.
	if e.Trace != nil {
		if _, ok := e.traces[r.Rule.Pos()]; !ok {
			if e.traces == nil {
				e.traces = make(map[token.Pos]*trace.Rule)
			}

			pos := r.Rule.Pos()
			if identPos, ok := e.ruleMap[pos]; ok {
				pos = identPos
			}

			// Store the trace
			e.traces[r.Rule.Pos()] = &trace.Rule{
				Desc:  r.Rule.Doc.Text(),
				Ident: ident,
				Pos:   pos,
				Root:  e.pushTrace(r.Rule.Expr),
			}
		}
	}

	raw := e.eval(r.Rule.Expr, r.Scope)

	// If we're tracing, then we need to restore the old trace.
	if e.Trace != nil {
		e.popTrace(raw)
	}

	// If the rule didn't result in a bool, it is an error
	if raw.Type() != object.BOOL && raw.Type() != object.UNDEFINED {
		e.err("rule evaluated to a non-boolean value", r.Rule.Expr, r.Scope)
	}

	r.Value = raw
	return raw
}

// ----------------------------------------------------------------------------
// Helpers

func (e *evalState) enterFrame(n ast.Node, s *object.Scope) *object.Scope {
	e.depth++
	if e.depth > e.MaxDepth {
		e.err(fmt.Sprintf("maximum stack depth %d reached", e.MaxDepth), n, s)
	}

	return object.NewScope(s)
}

func (e *evalState) exitFrame(ast.Node, *object.Scope) {
	e.depth--
}

// ----------------------------------------------------------------------------
// Error messages

const errNoMain = `%s: No 'main' assignment found.

You must assign a rule to the 'main' variable. This is the entrypoint for
a Sentinel policy. The result of this rule determines the result of the
entire policy.`

const errNoReturn = `No return statement found in the function!

A return statement is required to return a result from a function.`

const errTimeout = `Execution of policy timed out after %[1]s.

Policy execution is limited to %[1]s by the host system. Please modify the
policy to execute within this time.
`

const errUndefined = `Result value was 'undefined'.

This means that undefined behavior was experienced at one or more locations
and the result of your policy ultimately resulted in "undefined". This usually
represents a policy which doesn't handle all possible cases and should be
corrected.

The locations where the undefined behavior happened are:

%s`
