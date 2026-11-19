package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/protocol"
)

// How the sidebar groups the recent tasks, as the Codex app groups its
// threads: by project (the work folder of the task's agent), by agent, or
// not at all. The prefs keep the choice. No UI here, so the tests cover
// the rules; sidebar.go draws the groups.

// grouping is a way to group the tasks.
type grouping struct {
	id    string        // the prefs value
	label func() string // the Settings label, in the language shown
	menu  func() string // the item of the sidebar menu
}

var groupings = []grouping{
	{"project", locales.GroupProject, locales.GroupByProject},
	{"agent", locales.GroupAgent, locales.GroupByAgent},
	{"none", locales.GroupNone, locales.GroupByNone},
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
			return g.label()
		}
	}
	return ""
}

// groupingIDs are the prefs values of the groupings, for Settings.
func groupingIDs() []string {
	ids := make([]string, len(groupings))
	for i, g := range groupings {
		ids[i] = g.id
	}
	return ids
}

// taskGroup is a group of tasks in the sidebar. With no grouping there is
// one group, with no key and no title.
type taskGroup struct {
	key   string // stable across reloads: the prefs keep the closed groups by it
	title string
	tip   string
	icon  *ui.SVG
	tasks []protocol.Task
}

// groupTasks groups tasks by the grouping id. The groups and the tasks in
// them keep the order of tasks, newest first, so the group with the newest
// task comes first.
func groupTasks(tasks []protocol.Task, agents []protocol.Agent, id string) []taskGroup {
	if id == "none" {
		if len(tasks) == 0 {
			return nil
		}
		return []taskGroup{{tasks: tasks}}
	}
	var out []taskGroup
	at := map[string]int{}
	for _, j := range tasks {
		g := groupOf(j, agents, id)
		i, ok := at[g.key]
		if !ok {
			i = len(out)
			at[g.key] = i
			out = append(out, g)
		}
		out[i].tasks = append(out[i].tasks, j)
	}
	return out
}

// groupOf is the group of a task, without its tasks.
func groupOf(j protocol.Task, agents []protocol.Agent, id string) taskGroup {
	a, found := agentOf(j, agents)
	if id == "agent" {
		g := taskGroup{key: "agent:" + j.Agent, title: j.Agent, icon: agentIcon}
		if found {
			g.tip = locales.DirOn(a.Dir, a.Machine)
			if a.Role == "super" {
				g.icon = sparklesIcon
			}
		}
		return g
	}
	if !found || a.Dir == "" {
		// The agent was removed: its folder is not known any more.
		return taskGroup{key: "project:", title: locales.GroupOther(), tip: locales.GroupOtherTip(), icon: folderIcon}
	}
	dir := strings.TrimRight(a.Dir, "/") // "~/codes/" is "~/codes"
	return taskGroup{
		key:   "project:" + a.Machine + ":" + dir,
		title: folderName(a.Dir),
		tip:   locales.DirOn(a.Dir, a.Machine),
		icon:  folderIcon,
	}
}

// agentOf finds the agent of a task: by ID, else by name.
func agentOf(j protocol.Task, agents []protocol.Agent) (protocol.Agent, bool) {
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

// sidebarTasks are the tasks in the order the sidebar shows them.
func sidebarTasks(groups []taskGroup) []protocol.Task {
	var out []protocol.Task
	for _, g := range groups {
		out = append(out, g.tasks...)
	}
	return out
}
