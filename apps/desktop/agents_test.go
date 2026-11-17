package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The detail of an agent shows its instructions in a text area. Save and
// Cancel show only when the text changed; Cancel puts the text back.
func TestInstructionsEdit(t *testing.T) {
	d := sampleDesktop(t)
	d.page, d.agentSel = pageAgents, "reviewer"
	tt := ui.NewTester(d.view, 1240, 820)
	const saved = "Review only the diff. Report each bug with a failing test; do not change the style."
	if d.instr.text != saved || tt.HasText("Save") {
		t.Fatalf("draft = %q, Save shown = %v", d.instr.text, tt.HasText("Save"))
	}
	d.instr.text = "Be brief."
	tt.Frame()
	if !tt.HasText("Save") || !tt.HasText("Cancel") {
		t.Fatalf("changed text: no Save and Cancel in %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if d.instr.text != saved || tt.HasText("Save") {
		t.Fatalf("after Cancel: draft = %q, Save shown = %v", d.instr.text, tt.HasText("Save"))
	}

	// Another agent starts its own draft.
	d.agentSel = "coder"
	tt.Frame()
	if d.instr.agent != "a1" || d.instr.text != "" {
		t.Fatalf("draft of coder = %+v", d.instr)
	}
}
