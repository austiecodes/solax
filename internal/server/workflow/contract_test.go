package workflow

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/austiecodes/solax/internal/js"
	"github.com/austiecodes/solax/pkg/protocol"
)

// workflow.d.ts in the solax-workflow skill is what agents read. These
// tests keep it equal to what the server gives.
const dtsPath = "../../assets/plugins/shared/skills/solax-workflow/references/workflow.d.ts"

func readDTS(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(dtsPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// members returns the member names of one interface in the d.ts.
func members(t *testing.T, dts, name string) []string {
	t.Helper()
	start := strings.Index(dts, "interface "+name+" {")
	if start < 0 {
		t.Fatalf("workflow.d.ts has no interface %s", name)
	}
	end := strings.Index(dts[start:], "\n}")
	body := dts[start : start+end]
	re := regexp.MustCompile(`(?m)^  (\w+)\??[:<(]`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	slices.Sort(out)
	return out
}

// jsonFields returns the JSON names of the fields of a struct.
func jsonFields(v any) []string {
	var out []string
	t := reflect.TypeOf(v)
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func TestDTSCompiles(t *testing.T) {
	if _, err := js.Compile(context.Background(), "workflow.d.ts", readDTS(t), js.DefaultLimits); err != nil {
		t.Fatal(err)
	}
}

func TestDTSMatchesPrelude(t *testing.T) {
	ctx := context.Background()
	prelude, err := js.Compile(ctx, "solax:prelude", preludeSource, js.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := js.Compile(ctx, "entry", `import { makeWf } from "solax:prelude";
export function keys() { return Object.keys(makeWf({})).sort(); }`, js.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	linked, err := js.Link(entry, map[string]*js.Module{"solax:prelude": prelude})
	if err != nil {
		t.Fatal(err)
	}
	vm, err := js.New(ctx, linked, js.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	out, err := vm.Call(ctx, "keys")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if want := members(t, readDTS(t), "Workflow"); !slices.Equal(got, want) {
		t.Fatalf("wf has %v, workflow.d.ts says %v", got, want)
	}
}

func TestDTSMatchesGoTypes(t *testing.T) {
	dts := readDTS(t)
	for name, v := range map[string]any{
		"TaskOutcome":    outcome{},
		"TaskOpts":       taskOpts{},
		"AgentInfo":      agentInfo{},
		"RecalledObject": protocol.Object{},
		"WorkflowMeta":   protocol.WorkflowMeta{},
	} {
		if got, want := jsonFields(v), members(t, dts, name); !slices.Equal(got, want) {
			t.Errorf("%s: Go has %v, workflow.d.ts says %v", name, got, want)
		}
	}
}
