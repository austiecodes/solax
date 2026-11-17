package discover

import (
	"testing"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

func machine(id, name string, online bool, kinds ...ir.Kind) protocol.Machine {
	m := protocol.Machine{ID: id, Name: name, State: protocol.MachineApproved, Online: online}
	for _, k := range kinds {
		m.Drivers = append(m.Drivers, protocol.DriverProbe{Kind: k, Version: string(k) + " 1.0", OK: true})
	}
	return m
}

func TestSuggestSkipsUsedAndBroken(t *testing.T) {
	mac := machine("mch_1", "Ada-MacBook-Pro.local", true, ir.KindPi, ir.KindCodex, ir.KindClaude)
	mac.Drivers = append(mac.Drivers, protocol.DriverProbe{Kind: "mycc", Problem: "not found"})
	agents := []protocol.Agent{{Name: "router", MachineID: "mch_1", Driver: ir.KindPi, Role: "super"}}

	got := Suggest([]protocol.Machine{mac}, agents)
	if len(got) != 2 {
		t.Fatalf("got %+v, want codex and claude", got)
	}
	for i, want := range []ir.Kind{ir.KindCodex, ir.KindClaude} {
		s := got[i]
		if s.Driver != want || s.Name != string(want) || s.MachineID != "mch_1" || s.Super {
			t.Errorf("suggestion %d = %+v, want %s", i, s, want)
		}
	}
}

func TestSuggestSkipsOfflineAndPending(t *testing.T) {
	off := machine("mch_1", "a", false, ir.KindClaude)
	pending := machine("mch_2", "b", true, ir.KindClaude)
	pending.State = protocol.MachinePending
	if got := Suggest([]protocol.Machine{off, pending}, nil); len(got) != 0 {
		t.Fatalf("got %+v, want none", got)
	}
}

// With no Super Agent, pi is the one, as in the README.
func TestSuggestMarksOneSuper(t *testing.T) {
	got := Suggest([]protocol.Machine{machine("mch_1", "a", true, ir.KindClaude, ir.KindPi)}, nil)
	if len(got) != 2 || got[0].Super || !got[1].Super || got[1].Driver != ir.KindPi {
		t.Fatalf("got %+v, want pi as the Super Agent", got)
	}
	got = Suggest([]protocol.Machine{machine("mch_1", "a", true, "mycc")}, nil)
	if len(got) != 1 || !got[0].Super {
		t.Fatalf("got %+v, want the only one as the Super Agent", got)
	}
}

// A second machine with the same CLI gets the machine in the name.
func TestSuggestNamesAreFree(t *testing.T) {
	ms := []protocol.Machine{
		machine("mch_1", "Ada-MacBook-Pro.local", true, ir.KindClaude),
		machine("mch_2", "build box", true, ir.KindClaude),
		machine("mch_3", "build box", true, ir.KindClaude),
		machine("mch_4", "日本", true, ir.KindClaude),
	}
	agents := []protocol.Agent{{Name: "claude", MachineID: "mch_9", Driver: ir.KindCodex, Role: "super"}}
	got := Suggest(ms, agents)
	want := []string{"claude-ada-macbook-pro", "claude-build-box", "claude-2", "claude-3"}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, s := range got {
		if s.Name != want[i] {
			t.Errorf("name %d = %q, want %q", i, s.Name, want[i])
		}
		if !nameRule.MatchString(s.Name) {
			t.Errorf("name %q breaks the server rule", s.Name)
		}
	}
}

func TestRequestDefaults(t *testing.T) {
	s := Suggestion{Name: "claude", Driver: ir.KindClaude, MachineID: "mch_1"}
	r := s.Request("", "")
	if r.Dir != DefaultDir || r.Description != Description(ir.KindClaude, false) || r.Machine != "mch_1" || r.Super {
		t.Fatalf("request = %+v", r)
	}
	r = s.Request("~/src", " mine ")
	if r.Dir != "~/src" || r.Description != "mine" {
		t.Fatalf("request = %+v", r)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Ada-MacBook-Pro.local": "ada-macbook-pro",
		"  build  box ":         "build-box",
		"--x--":                 "x",
		"日本":                    "",
		"ws_01.corp":            "ws-01",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}
