// Package discover suggests agents for the agent CLIs that the daemons
// found and that no agent uses yet. The CLI ("solax agent discover") and
// the desktop app show the same suggestions, and add one with a click.
package discover

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// DefaultDir is the work folder of a suggested agent. The daemon expands
// "~" to the home folder of the machine.
const DefaultDir = "~"

// Suggestion is an agent that can be set up as is: an agent CLI that a
// daemon found, on its machine, with a free name.
type Suggestion struct {
	Name      string  `json:"name"`
	Driver    ir.Kind `json:"driver"`
	Version   string  `json:"version"`
	MachineID string  `json:"machine_id"`
	Machine   string  `json:"machine"` // machine name
	// Super is true for one suggestion when there is no Super Agent yet:
	// set up with it, the agent becomes the one you talk to.
	Super bool `json:"super,omitempty"`
}

// Request is the request that sets the suggestion up, with a work folder
// and a description; empty ones take DefaultDir and Description.
func (s Suggestion) Request(dir, desc string) protocol.CreateAgentRequest {
	if strings.TrimSpace(dir) == "" {
		dir = DefaultDir
	}
	if strings.TrimSpace(desc) == "" {
		desc = Description(s.Driver, s.Super)
	}
	return protocol.CreateAgentRequest{
		Machine: s.MachineID, Name: s.Name, Driver: s.Driver, Dir: dir,
		Description: strings.TrimSpace(desc), Super: s.Super,
	}
}

// superOrder is the order in which a driver is chosen to be the Super
// Agent, as the README sets it up: pi first.
var superOrder = []ir.Kind{ir.KindPi, ir.KindClaude, ir.KindCodex}

// Suggest returns an agent for each working agent CLI of each online,
// approved machine, unless an agent of that machine uses it already. The
// order is the order of the machines, then of the drivers they report.
func Suggest(machines []protocol.Machine, agents []protocol.Agent) []Suggestion {
	taken := map[string]bool{}
	used := map[string]bool{} // machine ID + driver
	hasSuper := false
	for _, a := range agents {
		taken[a.Name] = true
		used[a.MachineID+"\x00"+string(a.Driver)] = true
		hasSuper = hasSuper || a.Role == "super"
	}
	var out []Suggestion
	for _, m := range machines {
		if m.State != protocol.MachineApproved || !m.Online {
			continue
		}
		for _, d := range m.Drivers {
			if !d.OK || used[m.ID+"\x00"+string(d.Kind)] {
				continue
			}
			used[m.ID+"\x00"+string(d.Kind)] = true
			name := freeName(string(d.Kind), m.Name, taken)
			taken[name] = true
			out = append(out, Suggestion{Name: name, Driver: d.Kind, Version: d.Version, MachineID: m.ID, Machine: m.Name})
		}
	}
	if !hasSuper {
		markSuper(out)
	}
	return out
}

// markSuper makes the first suggestion of the driver that comes first in
// superOrder the Super Agent; with none of those, the first suggestion.
func markSuper(ss []Suggestion) {
	if len(ss) == 0 {
		return
	}
	best, rank := 0, len(superOrder)
	for i, s := range ss {
		if r := slices.Index(superOrder, s.Driver); r >= 0 && r < rank {
			best, rank = i, r
		}
	}
	ss[best].Super = true
}

// Description is the English description of a suggested agent. The Super
// Agent reads the descriptions to choose an agent for a job.
func Description(k ir.Kind, super bool) string {
	if super {
		return "Solax: talks to you and gives jobs to the other agents"
	}
	switch k {
	case ir.KindClaude:
		return "Claude Code: writes, reviews and refactors code"
	case ir.KindCodex:
		return "Codex: writes code, runs commands and tests"
	case ir.KindPi:
		return "pi: a light coding agent for quick tasks"
	}
	return string(k) + " (a fork of Claude Code): writes, reviews and refactors code"
}

// nameRule is the rule of the server for agent names.
var nameRule = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// freeName is the driver kind if no agent has it, else the kind and the
// machine, else the kind and a number.
func freeName(kind, machine string, taken map[string]bool) string {
	free := func(n string) bool { return nameRule.MatchString(n) && !taken[n] }
	if free(kind) {
		return kind
	}
	if slug := Slug(machine); slug != "" {
		if n := clipName(kind + "-" + slug); free(n) {
			return n
		}
	}
	for i := 2; ; i++ {
		if n := clipName(kind + "-" + strconv.Itoa(i)); free(n) {
			return n
		}
	}
}

// Slug turns a machine name into a part of an agent name: the host name
// up to the first dot, in lower case, other characters as dashes.
// "Ada-MacBook-Pro.local" is "ada-macbook-pro".
func Slug(machine string) string {
	host, _, _ := strings.Cut(strings.ToLower(machine), ".")
	var b strings.Builder
	dash := false
	for _, r := range host {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// clipName cuts a name to the 32 characters the server allows, without a
// dash at the end.
func clipName(n string) string {
	if len(n) > 32 {
		n = n[:32]
	}
	return strings.TrimRight(n, "-")
}
