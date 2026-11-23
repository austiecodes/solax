package js

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MoeForge/moejs"
)

func mustVM(t *testing.T, src string, lim Limits) *VM {
	t.Helper()
	ctx := context.Background()
	m, err := Compile(ctx, "main.ts", src, lim)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	vm, err := New(ctx, m, lim)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(vm.Close)
	return vm
}

func TestCompileSyntaxError(t *testing.T) {
	src := "export const a: number = 1;\nexport const b = ;\nexport const c = 3;\n"
	_, err := Compile(context.Background(), "bad.ts", src, DefaultLimits)
	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *SyntaxError", err)
	}
	if se.File != "bad.ts" || se.Line != 2 || se.Column != 18 {
		t.Fatalf("position = %s:%d:%d, want bad.ts:2:18", se.File, se.Line, se.Column)
	}
}

func TestCompileSourceLimit(t *testing.T) {
	lim := DefaultLimits
	lim.Source = 10
	_, err := Compile(context.Background(), "big.ts", "export const a = 12345678;", lim)
	if err == nil || !strings.Contains(err.Error(), "more than the limit") {
		t.Fatalf("err = %v, want a size error", err)
	}
}

func TestCallTypeScriptAndHost(t *testing.T) {
	vm := mustVM(t, `
interface Args { names: string[] }
export async function run(host: any, args: Args): Promise<string[]> {
  host.log("start");
  const out = await Promise.all(args.names.map((n: string) => host.greet(n)));
  return out;
}`, DefaultLimits)

	var mu sync.Mutex
	var seqs []string
	host := Host{
		"log": Func(func(c Call) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			seqs = append(seqs, "log:"+string(c.Args[0]))
			return nil, nil
		}),
		"greet": AsyncFunc(func(ctx context.Context, c Call) (any, error) {
			var name string
			if err := json.Unmarshal(c.Args[0], &name); err != nil {
				return nil, err
			}
			time.Sleep(10 * time.Millisecond) // waits off the loop
			mu.Lock()
			seqs = append(seqs, name)
			mu.Unlock()
			return map[string]any{"hello": name, "seq": c.Seq}, nil
		}),
	}
	got, err := vm.Call(context.Background(), "run", host, map[string]any{"names": []string{"a", "b"}})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	want := `[{"hello":"a","seq":1},{"hello":"b","seq":2}]` // the log has its own count
	if string(got) != want {
		t.Fatalf("result = %s, want %s", got, want)
	}
	if seqs[0] != `log:"start"` {
		t.Fatalf("first host call = %q, want the log", seqs[0])
	}
}

func TestExport(t *testing.T) {
	vm := mustVM(t, `export const meta = { name: "x", phases: [{ title: "A" }] };
export async function run() { return null; }`, DefaultLimits)
	var meta struct {
		Name   string
		Phases []struct{ Title string }
	}
	ok, err := vm.Export(context.Background(), "meta", &meta)
	if err != nil || !ok {
		t.Fatalf("Export = %v, %v", ok, err)
	}
	if meta.Name != "x" || len(meta.Phases) != 1 || meta.Phases[0].Title != "A" {
		t.Fatalf("meta = %+v", meta)
	}
	if ok, err := vm.Export(context.Background(), "nope", &meta); ok || err != nil {
		t.Fatalf("missing export = %v, %v", ok, err)
	}
}

func TestExportNotJSON(t *testing.T) {
	vm := mustVM(t, `export const meta = { name: "x", f: () => 1 };`, DefaultLimits)
	var v any
	if _, err := vm.Export(context.Background(), "meta", &v); err == nil {
		t.Fatal("Export of a function member: want an error")
	}
}

func TestGuards(t *testing.T) {
	cases := map[string]string{
		"Date.now":      "Date.now()",
		"new Date":      "new Date()",
		"Math.random":   "Math.random()",
		"eval":          `eval("1")`,
		"Function":      `new Function("return 1")()`,
		"no setTimeout": "setTimeout(() => {}, 1)",
	}
	for name, expr := range cases {
		t.Run(name, func(t *testing.T) {
			vm := mustVM(t, "export async function run() { return "+expr+"; }", DefaultLimits)
			_, err := vm.Call(context.Background(), "run")
			var je *Error
			if !errors.As(err, &je) {
				t.Fatalf("err = %v, want *Error", err)
			}
		})
	}
	vm := mustVM(t, "export async function run() { return new Date(0).toISOString(); }", DefaultLimits)
	got, err := vm.Call(context.Background(), "run")
	if err != nil || string(got) != `"1970-01-01T00:00:00.000Z"` {
		t.Fatalf("new Date(0) = %s, %v", got, err)
	}
}

func TestErrorStackUsesTypeScriptLines(t *testing.T) {
	vm := mustVM(t, "type T = { a: number };\n\nexport async function run() {\n  throw new TypeError(\"bad\");\n}", DefaultLimits)
	_, err := vm.Call(context.Background(), "run")
	var je *Error
	if !errors.As(err, &je) {
		t.Fatalf("err = %v, want *Error", err)
	}
	if je.Message != "TypeError: bad" {
		t.Fatalf("message = %q", je.Message)
	}
	if !strings.Contains(je.Stack, "main.ts:4") {
		t.Fatalf("stack does not name main.ts:4:\n%s", je.Stack)
	}
}

func TestHostErrorRejects(t *testing.T) {
	vm := mustVM(t, `export async function run(h: any) {
  try { await h.fail(); } catch (e) { return "caught: " + e.message; }
}`, DefaultLimits)
	host := Host{"fail": AsyncFunc(func(context.Context, Call) (any, error) {
		return nil, errors.New("no agent")
	})}
	got, err := vm.Call(context.Background(), "run", host)
	if err != nil || string(got) != `"caught: no agent"` {
		t.Fatalf("result = %s, %v", got, err)
	}
}

func TestBusyLimit(t *testing.T) {
	lim := DefaultLimits
	lim.Busy = 50 * time.Millisecond
	vm := mustVM(t, "export async function run() { for (;;) {} }", lim)
	_, err := vm.Call(context.Background(), "run")
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
}

func TestWaitDoesNotCountAsBusy(t *testing.T) {
	lim := DefaultLimits
	lim.Busy = 50 * time.Millisecond
	vm := mustVM(t, "export async function run(h: any) { await h.slow(); await h.slow(); return 1; }", lim)
	host := Host{"slow": AsyncFunc(func(context.Context, Call) (any, error) {
		time.Sleep(80 * time.Millisecond)
		return nil, nil
	})}
	if _, err := vm.Call(context.Background(), "run", host); err != nil {
		t.Fatalf("Call: %v", err)
	}
}

func TestMemoryLimit(t *testing.T) {
	lim := DefaultLimits
	lim.Memory = 4 << 20
	vm := mustVM(t, `export async function run() {
  const keep: string[] = [];
  for (let i = 0; ; i++) keep.push("x".repeat(1000) + i);
}`, lim)
	_, err := vm.Call(context.Background(), "run")
	if !errors.Is(err, moejs.ErrMemoryLimit) {
		t.Fatalf("err = %v, want ErrMemoryLimit", err)
	}
}

func TestResultLimit(t *testing.T) {
	lim := DefaultLimits
	lim.Result = 10
	vm := mustVM(t, `export async function run() { return "a long string"; }`, lim)
	if _, err := vm.Call(context.Background(), "run"); err == nil {
		t.Fatal("want a result size error")
	}
}

func TestPendingForever(t *testing.T) {
	vm := mustVM(t, "export async function run() { await new Promise(() => {}); }", DefaultLimits)
	_, err := vm.Call(context.Background(), "run")
	if err == nil || !strings.Contains(err.Error(), "nothing settles") {
		t.Fatalf("err = %v", err)
	}
}

func TestCancelStopsWait(t *testing.T) {
	vm := mustVM(t, "export async function run(h: any) { await h.wait(); }", DefaultLimits)
	host := Host{"wait": AsyncFunc(func(ctx context.Context, _ Call) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := vm.Call(ctx, "run", host)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
}

func TestLinkOnlyEntryImports(t *testing.T) {
	ctx := context.Background()
	user, err := Compile(ctx, "user.ts", `export async function run(x: number) { return x * 2; }`, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := Compile(ctx, "entry.ts", `import { run } from "solax:user";
export async function main(x: number) { return { doubled: await run(x) }; }`, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	linked, err := Link(entry, map[string]*Module{"solax:user": user})
	if err != nil {
		t.Fatalf("Link: %v", err)
	}
	vm, err := New(ctx, linked, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	got, err := vm.Call(ctx, "main", 21)
	if err != nil || string(got) != `{"doubled":42}` {
		t.Fatalf("main = %s, %v", got, err)
	}

	sneaky, err := Compile(ctx, "user.ts", `import { x } from "solax:other"; export const y = x;`, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Link(entry, map[string]*Module{"solax:user": sneaky, "solax:other": user})
	var se *SyntaxError
	if !errors.As(err, &se) || !strings.Contains(se.Message, "imports are not allowed") {
		t.Fatalf("err = %v, want imports are not allowed", err)
	}
}
