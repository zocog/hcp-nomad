package sentinel

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/hashicorp/sentinel/lang/ast"
	"github.com/hashicorp/sentinel/lang/semantic"
	"github.com/hashicorp/sentinel/lang/token"
	"github.com/hashicorp/sentinel/runtime/eval"
	"github.com/mitchellh/hashstructure"
)

// Module represents a single module.
//
// A module is a subset of a policy's functionality, albeit moderately
// simplified to represent that a module is just an AST at its heart that
// behaves like an import.
//
// A module can be either ready or not ready. A ready module is a
// module that has a loaded AST and configured imports. A module can
// be readied by acquiring a lock, calling the Set* methods, and then
// calling the SetReady method, which will mark the module as ready
// and switch the write lock to a read lock.
//
// Module methods are safe for concurrent access. A module is protected with
// a reader/writer lock. The lock must be held for using the module.
//
// Modules are not usually created directly. Instead, the Sentinel.Module
// method is used to create and lock the module. The returned value from this
// method always returns a locked module. The policy module be unlocked upon
// completion.
type Module struct {
	// Module has a RWMutex. This is unexported since locking should be
	// completely handled by the Sentinel structure.
	rwmutex sync.RWMutex

	ready    uint32                   // atomic set, 0 == not ready, 1 == pending, 2 == ready
	compiled *eval.Compiled           // compiled module to execute
	imports  map[string]*policyImport // available imports
	name     string                   // human-friendly name

	// Ready state. This is only used/set if the policy is not ready.
	readyLock sync.Mutex
	file      *ast.File      // Parsed file to execute
	fileSet   *token.FileSet // FileSet for positional information
}

//-------------------------------------------------------------------
// Getters only valid when Ready, they all assume a read lock is
// already held
//

// File returns the ast.File associated with this Policy. This can be used
// to avoid recompiling the File if it is shared with other policies or needs
// to be used for any other reason.
func (m *Module) File() *ast.File {
	return m.file
}

// FileSet returns the FileSet associated with this Policy. This can be
// used to turn token.Pos values into full positions.
func (m *Module) FileSet() *token.FileSet {
	return m.fileSet
}

// Name returns the name of this module that was set with SetName.
// This will default to the ID given to Sentinel.Policy but can be
// overwritten.
func (m *Module) Name() string {
	return m.name
}

// Doc returns the docstring value for this module.
func (m *Module) Doc() string {
	return m.file.Doc.Text()
}

//-------------------------------------------------------------------
// Readiness
//

// Ready returns whether the module is ready or not. This can be called
// concurrently without any lock held.
func (m *Module) Ready() bool {
	return atomic.LoadUint32(&m.ready) == readyReady
}

// SetReady marks a Policy as ready and swaps the write lock to a read lock,
// allowing waiters to begin using the module.
//
// This should only be called if Ready() is false and after the other Set*
// methods are called to setup the state of the Policy.
//
// The error return value should be checked. This will be nil if the module
// was successfuly set as ready. If it is non-nil, the module is not
// ready. In either case, the module must be explicitly unlocked still.
func (m *Module) SetReady() error {
	// Compile the module.
	if m.file == nil {
		return errors.New("module file must be set to set module ready")
	}
	cf, err := eval.Compile(&eval.CompileOpts{
		File:         m.file,
		FileSet:      m.fileSet,
		SkipCheckers: []semantic.Checker{&semantic.CheckMain{}},
	})
	if err != nil {
		return err
	}
	m.compiled = cf

	// Hash all the imports
	for k, v := range m.imports {
		if v.Hash == 0 {
			// New import configuration, set it up
			hash, err := hashstructure.Hash(v.Config, nil)
			if err != nil {
				return fmt.Errorf(
					"error setting module configuration for import %q: %v",
					k, v)
			}

			v.Hash = hash
		}
	}

	// Mark as ready
	if !atomic.CompareAndSwapUint32(&m.ready, readyNotReady, readyReady) {
		// It was already ready, just return since there is no way we
		// hold a write lock. We really should panic in this scenario, but
		// I didn't want to introduce a potential crash case.
		return errors.New("unable to set module ready, incorrect source state")
	}

	// Acquire read lock
	m.rwmutex.Unlock()
	m.rwmutex.RLock()

	// Unlock the ready lock
	m.readyLock.Unlock()

	return nil
}

// ResetReady makes the module not ready again. The lock must be held
// prior to calling this. If the write lock is already held, then this will
// return immediately. If a read lock is held, this will block until the
// write lock can be acquired.
//
// Once this returns, the Policy should be treated like a not ready module.
// SetReady should be called, Unlock should be called, etc.
//
// This will not reset any of the data or configuration associated with
// a module. You can call SetReady directly after this to retain the existing
// module.
func (m *Module) ResetReady() {
	// If we're not already ready, then just ignore this call. This is safe
	// because the precondition is that a lock MUST be held to call this.
	// If we have a read lock, this will be ready. If we have a write lock,
	// then we have an exclusive lock. In either case, we're safely handling
	// locks.
	if !m.Ready() {
		return
	}

	// Acquire readylock so only one writer can exist. If the module
	// is already not ready, then this will block waiting for the person
	// with the write lock to yield.
	m.readyLock.Lock()

	// We should have the read lock so unlock that first.
	m.rwmutex.RUnlock()

	// Grab a write lock on the rwmutex. This will only properly
	// happen once all the readers unlock.
	m.rwmutex.Lock()

	// Set not ready
	atomic.StoreUint32(&m.ready, readyNotReady)
}

// Lock locks the Module. It automatically grabs a reader or writer lock
// based on the value of Ready().
func (m *Module) Lock() {
	// Fast path: check if we're already Ready() and grab a reader lock.
	if m.Ready() {
		m.rwmutex.RLock()
		return
	}

	// Slow path: we're not ready. First, acquire the lock protecting ready
	// state. We have a secondary lock here so that we can grab it after
	// the read lock is acquired from SetReady.
	m.readyLock.Lock()

	// If it became ready, switch with a read lock. Otherwise, keep the writer
	if m.Ready() {
		m.readyLock.Unlock()
		m.rwmutex.RLock()
		return
	}

	// Not ready, grab the RW write lock to prevent any read locks.
	m.rwmutex.Lock()
}

// Unlock unlocks the Module.
func (m *Module) Unlock() {
	// If we're ready, it is always a reader unlock. This is always true
	// since the SetReady() function will swap a write lock with a read lock
	// and Lock won't allow a write lock unless its not ready.
	if m.Ready() {
		m.rwmutex.RUnlock()
	} else {
		m.rwmutex.Unlock()
		m.readyLock.Unlock()
	}
}

//-------------------------------------------------------------------
// Setters while not ready
//

// SetName sets a human-friendly name for this module. This is used
// for tracing and debugging. This name will default to the path
// given to Sentinel.Module but can be changed to anything else.
//
// The write lock must be held for this.
func (m *Module) SetName(name string) {
	m.name = name
}

// SetModule sets the parsed module file contents.
//
// The write lock must be held for this.
func (m *Module) SetModule(f *ast.File, fset *token.FileSet) {
	m.file = f
	m.fileSet = fset
}

//-------------------------------------------------------------------
// encoding/json
//

// MarshalJSON implements json.Marshaler. A module marshals only to
// its name.
func (m *Module) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.name)
}
