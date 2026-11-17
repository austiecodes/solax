package driver

import (
	"strings"
	"testing"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/pkg/ir"
)

func TestAllClaudeCompatible(t *testing.T) {
	ds, err := All("", "mycc", "/opt/tools/other-cc")
	if err != nil {
		t.Fatal(err)
	}
	var kinds []ir.Kind
	for _, d := range ds {
		kinds = append(kinds, d.Kind())
	}
	want := []ir.Kind{ir.KindPi, ir.KindCodex, ir.KindClaude, "mycc", "other-cc"}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
	for _, bad := range [][]string{{"My CC"}, {"/x/codex"}, {"pi"}, {"mycc", "/y/mycc"}} {
		if _, err := All("", bad...); err == nil {
			t.Errorf("All(%q) has no error", bad)
		}
	}
}

// A path named claude is the binary of the built-in: All keeps its
// kinds and their order.
func TestAllBuiltinBinary(t *testing.T) {
	ds, err := All("", "/opt/real/claude")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(ds); n != len(ir.BuiltinKinds) {
		t.Fatalf("%d drivers, want the %d built-ins", n, len(ir.BuiltinKinds))
	}
	d, err := agent.Find(ds, ir.KindClaude)
	if err != nil {
		t.Fatal(err)
	}
	if fp := d.(interface{ Fingerprint() string }).Fingerprint(); !strings.HasPrefix(fp, "/opt/real/claude:") {
		t.Fatalf("claude fingerprint = %q, want the configured path", fp)
	}
}
