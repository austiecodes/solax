// Package js runs untrusted TypeScript in a sandbox. It is built on moejs, a
// JavaScript engine in pure Go.
//
// A script has no I/O: it reaches the outside world only through the host
// functions that Go gives it. It cannot read the clock or random values and
// has no timers, so a script that runs again with the same host results takes
// the same path. Workflows depend on this for step replay.
//
// The usual flow is Compile, then Link (when Go adds its own modules), then
// New and VM.Call.
package js

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MoeForge/moejs"
)

// Limits bound what one script can use.
type Limits struct {
	Source  int           // bytes of source text
	Compile time.Duration // time to compile one module
	Memory  int64         // bytes the JavaScript heap keeps plus allocates
	// Busy bounds the time JavaScript runs without a wait. Time spent in a
	// wait (for a host function result) does not count.
	Busy   time.Duration
	Result int // bytes of the JSON result of VM.Call and VM.Export
}

// DefaultLimits suit a workflow script.
var DefaultLimits = Limits{
	Source:  64 << 10,
	Compile: 500 * time.Millisecond,
	Memory:  32 << 20,
	Busy:    500 * time.Millisecond,
	Result:  64 << 10,
}

// ErrBusy stops a script that runs longer than Limits.Busy without a wait.
var ErrBusy = errors.New("js: script ran too long without a wait")

// SyntaxError is an error in the source of a module.
type SyntaxError struct {
	File    string
	Line    int // 1-based
	Column  int // 1-based
	Message string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Message)
}

// Error is a JavaScript value that a script threw and did not catch.
type Error struct {
	Message string
	Stack   string // V8 format; positions refer to the TypeScript source
}

func (e *Error) Error() string { return e.Message }

// Module is one compiled module. It is immutable, and any number of VMs can
// load it.
type Module struct {
	mod *moejs.Module
}

// Compile compiles a TypeScript module. Types are removed, not checked. A
// source error is a *SyntaxError.
func Compile(ctx context.Context, name, src string, lim Limits) (*Module, error) {
	if lim.Source > 0 && len(src) > lim.Source {
		return nil, fmt.Errorf("js: %s is %d bytes, more than the limit of %d", name, len(src), lim.Source)
	}
	if lim.Compile > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, lim.Compile)
		defer cancel()
	}
	m, err := moejs.CompileWith(name, src, moejs.CompileOptions{TypeScript: true, Context: ctx})
	if err != nil {
		return nil, sourceError(err)
	}
	return &Module{mod: m}, nil
}

// Exports returns the names that the module exports.
func (m *Module) Exports() []string { return m.mod.Exports() }

// Link links entry with the modules it imports. deps maps an import
// specifier to its module. Only entry can import: an import in any other
// module fails, so a user script that Go links in can import nothing.
func Link(entry *Module, deps map[string]*Module) (*Module, error) {
	m, err := moejs.Link(entry.mod, func(ref moejs.Referrer, spec string) (*moejs.Module, error) {
		if ref != moejs.Referrer(entry.mod) {
			return nil, fmt.Errorf("imports are not allowed")
		}
		d, ok := deps[spec]
		if !ok {
			return nil, fmt.Errorf("no module %q", spec)
		}
		return d.mod, nil
	})
	if err != nil {
		return nil, sourceError(err)
	}
	return &Module{mod: m}, nil
}

// sourceError converts the source errors of moejs to *SyntaxError.
func sourceError(err error) error {
	var se *moejs.SyntaxError
	if errors.As(err, &se) {
		return &SyntaxError{File: se.File, Line: se.Line, Column: se.Column, Message: se.Message}
	}
	var re *moejs.ResolveError
	if errors.As(err, &re) {
		return &SyntaxError{File: re.File, Line: re.Line, Column: re.Column,
			Message: fmt.Sprintf("import %q: %v", re.Specifier, re.Err)}
	}
	return err
}
