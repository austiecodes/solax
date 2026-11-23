package supervisor

import (
	"testing"

	"github.com/austiecodes/solax/internal/agent/session"
	"github.com/austiecodes/solax/pkg/ir"
)

func TestRoute(t *testing.T) {
	caps := func(modes ...ir.InputMode) ir.Caps {
		c := ir.Caps{Native: map[ir.InputMode]bool{ir.ModePrompt: true}}
		for _, m := range modes {
			c.Native[m] = true
		}
		return c
	}
	starting := func() *session.Machine { return session.NewMachine() }
	idle := func() *session.Machine {
		m := session.NewMachine()
		m.Apply(ir.Event{Type: ir.SessionStarted})
		return m
	}
	busy := func() *session.Machine {
		m := idle()
		m.Apply(ir.Event{Type: ir.TurnStarted, TurnID: "t1"})
		return m
	}
	cases := []struct {
		name      string
		state     *session.Machine
		prompted  bool
		caps      ir.Caps
		in        ir.InputMode
		want      ir.InputMode
		wantQueue bool
	}{
		{"idle follow-up starts a turn", idle(), false, caps(), ir.ModeFollowUp, ir.ModePrompt, false},
		{"idle steer starts a turn", idle(), false, caps(ir.ModeSteer), ir.ModeSteer, ir.ModePrompt, false},
		{"busy native steer", busy(), false, caps(ir.ModeSteer), ir.ModeSteer, ir.ModeSteer, false},
		{"busy steer without native steer waits", busy(), false, caps(ir.ModeFollowUp), ir.ModeSteer, ir.ModeFollowUp, true},
		{"busy native follow-up", busy(), false, caps(ir.ModeFollowUp), ir.ModeFollowUp, ir.ModeFollowUp, false},
		{"busy follow-up without native waits", busy(), false, caps(), ir.ModeFollowUp, ir.ModeFollowUp, true},
		{"prompted idle session waits for its turn", idle(), true, caps(), ir.ModePrompt, ir.ModeFollowUp, true},
		{"starting session waits even with native steer", starting(), false, caps(ir.ModeSteer), ir.ModeSteer, ir.ModeFollowUp, true},
		{"interrupt goes as it is", busy(), false, caps(), ir.ModeInterrupt, ir.ModeInterrupt, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &running{state: c.state, prompted: c.prompted, caps: c.caps}
			got, queue := r.route(c.in)
			if got != c.want || queue != c.wantQueue {
				t.Fatalf("route(%q) = (%q, %v), want (%q, %v)", c.in, got, queue, c.want, c.wantQueue)
			}
		})
	}
}
