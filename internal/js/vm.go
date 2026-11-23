package js

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/eventloop"
)

// Host is an object of host functions that Go gives to a script. Each
// value is a Func or an AsyncFunc.
type Host map[string]any

// Func is a host function that runs on the script's goroutine and returns at
// once.
type Func func(c Call) (any, error)

// AsyncFunc is a host function that runs on its own goroutine, so it can
// wait. JavaScript gets a Promise of its result. ctx ends when the VM call
// that runs the script ends.
type AsyncFunc func(ctx context.Context, c Call) (any, error)

// Call is one call of a host function.
type Call struct {
	// Seq is the order of the call among the host calls of its kind in
	// the VM, from 1: Func calls and AsyncFunc calls have their own
	// counts, so adding a Func call (such as a log line) to a script does
	// not move the numbers of its AsyncFunc calls. A script that runs
	// again with the same host results makes the same calls in the same
	// order.
	Seq int
	// Args are the arguments as JSON. A missing or undefined argument is
	// null; trailing ones are left out.
	Args []json.RawMessage
}

// A result of a host function is converted for JavaScript as
// encoding/json writes it; a json.RawMessage is parsed as it is.

// VM runs one module in its own runtime and event loop. A VM is not safe for
// concurrent use.
type VM struct {
	rt   *moejs.Runtime
	loop *eventloop.Loop
	mod  *moejs.Module
	lim  Limits
	busy *time.Timer
	seqs [2]int          // calls of Func, of AsyncFunc
	ctx  context.Context // of the running VM call; async host calls get it
}

// New loads m in a new VM: the module's top-level code runs.
func New(ctx context.Context, m *Module, lim Limits) (*VM, error) {
	rt := moejs.NewRuntime(moejs.Options{
		MutableIntrinsics:  true, // the guard replaces Date and Math.random
		DisableDynamicCode: true,
		MemoryLimit:        lim.Memory,
	})
	vm := &VM{rt: rt, mod: m.mod, lim: lim, ctx: ctx}
	loop, err := eventloop.New(rt, eventloop.Options{OnIdle: vm.idle, OnWake: vm.wake})
	if err != nil {
		return nil, err
	}
	vm.loop = loop
	if lim.Busy > 0 {
		vm.busy = time.AfterFunc(lim.Busy, func() { loop.Interrupt(ErrBusy) })
		vm.busy.Stop()
	}
	g, err := guard()
	if err != nil {
		return nil, err
	}
	err = vm.run(ctx, func(rt *moejs.Runtime) error {
		if _, err := rt.RunScript(g); err != nil {
			return fmt.Errorf("js: guard: %w", err)
		}
		if err := rt.Load(m.mod); err != nil && !errors.Is(err, moejs.ErrModulePending) {
			return err
		}
		return nil
	})
	if err != nil {
		vm.Close()
		return nil, err
	}
	return vm, nil
}

// Close stops the VM. Pending host calls are dropped.
func (vm *VM) Close() {
	if vm.busy != nil {
		vm.busy.Stop()
	}
	vm.loop.Terminate()
}

// Export reads the export name as JSON into v. It reports false when the
// module does not export name.
func (vm *VM) Export(ctx context.Context, name string, v any) (bool, error) {
	val, ok := vm.rt.Export(name)
	if !ok {
		return false, nil
	}
	var b []byte
	err := vm.run(ctx, func(rt *moejs.Runtime) (err error) {
		b, err = vm.json(val)
		return err
	})
	if err != nil {
		return true, fmt.Errorf("js: export %s: %w", name, err)
	}
	return true, json.Unmarshal(b, v)
}

// Call calls the exported function export with args and waits until the
// Promise it returns settles. args are converted as encoding/json writes
// them, except a Host, which becomes an object of functions. The result is
// JSON; an uncaught JavaScript error is an *Error.
func (vm *VM) Call(ctx context.Context, export string, args ...any) (json.RawMessage, error) {
	h, err := vm.mod.Hook(export)
	if err != nil {
		return nil, fmt.Errorf("js: export %s: %w", export, err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // ends the async host calls of this call
	var res moejs.Value
	err = vm.run(ctx, func(rt *moejs.Runtime) error {
		vals := make([]moejs.Value, len(args))
		for i, a := range args {
			v, err := vm.value(a)
			if err != nil {
				return err
			}
			vals[i] = v
		}
		res, err = rt.Call(h, vals...)
		return err
	})
	if err != nil {
		return nil, err
	}
	if state, v, ok := moejs.PromiseResult(res); ok {
		switch state {
		case moejs.PromisePending:
			return nil, errors.New("js: the script waits on a promise that nothing settles")
		case moejs.PromiseRejected:
			return nil, vm.error(&moejs.Exception{Value: v})
		}
		res = v
	}
	var b []byte
	err = vm.run(ctx, func(rt *moejs.Runtime) (err error) {
		b, err = vm.json(res)
		return err
	})
	return b, err
}

// run runs fn and then the event loop until nothing keeps it alive.
func (vm *VM) run(ctx context.Context, fn func(*moejs.Runtime) error) error {
	vm.rt.ClearInterrupt()
	vm.ctx = ctx
	stop := context.AfterFunc(ctx, func() { vm.loop.Interrupt(context.Cause(ctx)) })
	defer stop()
	vm.wake()
	err := vm.loop.Run(fn)
	vm.idle()
	return vm.error(err)
}

func (vm *VM) idle() {
	if vm.busy != nil {
		vm.busy.Stop()
	}
}

func (vm *VM) wake() error {
	if vm.busy != nil {
		vm.busy.Reset(vm.lim.Busy)
	}
	return nil
}

// json writes v as strict JSON within Limits.Result. undefined is null.
func (vm *VM) json(v moejs.Value) ([]byte, error) {
	if v.IsUndefined() {
		return []byte("null"), nil
	}
	b, err := vm.rt.AppendJSONWith(nil, v, moejs.JSONOptions{Strict: true})
	if err != nil {
		return nil, err
	}
	if vm.lim.Result > 0 && len(b) > vm.lim.Result {
		return nil, fmt.Errorf("js: result is %d bytes, more than the limit of %d", len(b), vm.lim.Result)
	}
	return b, nil
}

// value converts a Go argument for JavaScript.
func (vm *VM) value(a any) (moejs.Value, error) {
	h, ok := a.(Host)
	if !ok {
		return vm.rt.FromGo(a)
	}
	obj := make(map[string]any, len(h))
	for name, f := range h {
		switch f := f.(type) {
		case Func:
			obj[name] = vm.rt.Function(name, 0, vm.sync(f))
		case AsyncFunc:
			obj[name] = vm.rt.Function(name, 0, vm.async(f))
		default:
			return moejs.Undefined(), fmt.Errorf("js: host %s is a %T, not a Func or AsyncFunc", name, f)
		}
	}
	return vm.rt.FromGo(obj)
}

func (vm *VM) sync(f Func) moejs.NativeFunc {
	return func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		c, err := vm.call(0, args)
		if err != nil {
			return moejs.Undefined(), err
		}
		v, err := f(c)
		if err != nil {
			return moejs.Undefined(), err
		}
		return vm.rt.FromGo(v)
	}
}

func (vm *VM) async(f AsyncFunc) moejs.NativeFunc {
	return func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		c, err := vm.call(1, args)
		if err != nil {
			return moejs.Undefined(), err
		}
		p, settle := vm.loop.NewPromise()
		ctx := vm.ctx
		go func() {
			v, err := f(ctx, c)
			settle(func(rt *moejs.Runtime) (moejs.Value, error) {
				if err != nil {
					return moejs.Undefined(), err
				}
				return rt.FromGo(v)
			})
		}()
		return p, nil
	}
}

// call numbers a host call of kind (0 Func, 1 AsyncFunc) and converts its
// arguments to JSON.
func (vm *VM) call(kind int, args []moejs.Value) (Call, error) {
	vm.seqs[kind]++
	n := len(args)
	for n > 0 && args[n-1].IsUndefined() {
		n--
	}
	c := Call{Seq: vm.seqs[kind], Args: make([]json.RawMessage, n)}
	for i := range n {
		b, err := vm.json(args[i])
		if err != nil {
			return Call{}, fmt.Errorf("argument %d: %w", i+1, err)
		}
		c.Args[i] = b
	}
	return c, nil
}

// error converts a thrown JavaScript value to *Error. Other errors, such as
// interrupts (ErrBusy, a memory limit, a done context), stay as they are.
func (vm *VM) error(err error) error {
	var exc *moejs.Exception
	if !errors.As(err, &exc) {
		return err
	}
	msg := exc.Message()
	if n := exc.Name(); n != "" && n != msg {
		msg = n + ": " + msg
	}
	return &Error{Message: msg, Stack: vm.rt.StackTrace(exc)}
}

// guardSource removes what would let a script take another path when it
// runs again: the clock, random values and timers.
const guardSource = `"use strict";
(() => {
  const no = (what) => () => {
    throw new Error(what + " is not allowed: a replayed script must take the same path. Pass the value in args.");
  };
  const RealDate = Date;
  RealDate.now = no("Date.now()");
  Math.random = no("Math.random()");
  globalThis.Date = new Proxy(RealDate, {
    construct(target, args, newTarget) {
      if (args.length === 0) no("new Date()")();
      return Reflect.construct(target, args, newTarget);
    },
    apply() { no("Date()")(); },
  });
  for (const name of ["setTimeout", "setInterval", "setImmediate", "clearTimeout", "clearInterval", "clearImmediate"]) {
    delete globalThis[name];
  }
})();
`

var guard = sync.OnceValues(func() (*moejs.Script, error) {
	return moejs.CompileScript("solax:guard", guardSource)
})
