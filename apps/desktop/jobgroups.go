package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/pkg/protocol"
)

// How the sidebar groups the recent jobs, as the Codex app groups its
// threads: by project (the work folder of the job's agent), by agent, or
// not at all. The prefs keep the choice. No UI here, so the tests cover
// the rules; sidebar.go draws the groups.

// grouping is a way to group the jobs.
type grouping struct {
	id    string // the prefs value
	label string // the menu and Settings label
}

var groupings = []grouping{
	{"project", "Project"},
	{"agent", "Agent"},
	{"none", "None"},
}

// defaultGrouping is the Codex app's default.
const defaultGrouping = "project"

// validGrouping tells whether id names a grouping.
func validGrouping(id string) bool {
	for _, g := range groupings {
		if g.id == id {
			return true
		}
	}
	return false
}

// groupingLabel is the label of a grouping, for Settings.
func groupingLabel(id string) string {
	for _, g := range groupings {
		if g.id == id {
			return g.label
		}
	}
	return ""
}

// groupingByLabel is the grouping of a label, from Settings.
func groupingByLabel(label string) string {
	for _, g := range groupings {
		if g.label == label {
			return g.id
		}
	}
	return defaultGrouping
}

// jobGroup is a group of jobs in the sidebar. With no grouping there is
// one group, with no key and no title.
type jobGroup struct {
	key   string // stable across reloads: the prefs keep the closed groups by it
	title string
	tip   string
	icon  *ui.SVG
	jobs  []protocol.Job
}

// groupJobs groups jobs by the grouping id. The groups and the jobs in
// them keep the order of jobs, newest first, so the group with the newest
// job comes first.
func groupJobs(jobs []protocol.Job, agents []protocol.Agent, id string) []jobGroup {
	if id == "none" {
		if len(jobs) == 0 {
			return nil
		}
		return []jobGroup{{jobs: jobs}}
	}
	var out []jobGroup
	at := map[string]int{}
	for _, j := range jobs {
		g := groupOf(j, agents, id)
		i, ok := at[g.key]
		if !ok {
			i = len(out)
			at[g.key] = i
			out = append(out, g)
		}
		out[i].jobs = append(out[i].jobs, j)
	}
	return out
}

// groupOf is the group of a job, without its jobs.
func groupOf(j protocol.Job, agents []protocol.Agent, id string) jobGroup {
	a, found := agentOf(j, agents)
	if id == "agent" {
		g := jobGroup{key: "agent:" + j.Agent, title: j.Agent, icon: agentIcon}
		if found {
			g.tip = a.Dir + " on " + a.Machine
			if a.Role == "super" {
				g.icon = sparklesIcon
			}
		}
		return g
	}
	if !found || a.Dir == "" {
		// The agent was removed: its folder is not known any more.
		return jobGroup{key: "project:", title: "Other", tip: "Jobs of agents that were removed", icon: folderIcon}
	}
	dir := strings.TrimRight(a.Dir, "/") // "~/codes/" is "~/codes"
	return jobGroup{
		key:   "project:" + a.Machine + ":" + dir,
		title: folderName(a.Dir),
		tip:   a.Dir + " on " + a.Machine,
		icon:  folderIcon,
	}
}

// agentOf finds the agent of a job: by ID, else by name.
func agentOf(j protocol.Job, agents []protocol.Agent) (protocol.Agent, bool) {
	for _, a := range agents {
		if j.AgentID != "" && a.ID == j.AgentID {
			return a, true
		}
	}
	for _, a := range agents {
		if a.Name == j.Agent {
			return a, true
		}
	}
	return protocol.Agent{}, false
}

// folderName is the last element of a folder path: "app" for
// "~/src/app", "~" for "~".
func folderName(dir string) string {
	dir = strings.TrimRight(dir, "/")
	if dir == "" {
		return "/"
	}
	return filepath.Base(dir)
}

// sidebarJobs are the jobs in the order the sidebar shows them.
func sidebarJobs(groups []jobGroup) []protocol.Job {
	var out []protocol.Job
	for _, g := range groups {
		out = append(out, g.jobs...)
	}
	return out
}
