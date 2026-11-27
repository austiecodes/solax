package main

import (
	"testing"

	"github.com/austiecodes/solax/pkg/protocol"
)

// Tasks group by their project, or by agent, in the order of their newest
// task; a task without a project goes to "No project". A removed project
// still names its group.
func TestGroupTasks(t *testing.T) {
	agents := []protocol.Agent{
		{ID: "a1", Name: "coder", Machine: "mac"},
		{ID: "a2", Name: "helper", Machine: "mac"},
		{ID: "a3", Name: "reviewer", Machine: "box"},
	}
	projects := []protocol.Project{
		{ID: "p1", Name: "codes", Machine: "mac", Path: "~/codes"},
		{ID: "p2", Name: "app", Machine: "box", Path: "~/src/app"},
	}
	tasks := []protocol.Task{
		{ID: "j1", Agent: "reviewer", AgentID: "a3", Project: "app", ProjectID: "p2"},
		{ID: "j2", Agent: "coder", AgentID: "a1", Project: "codes", ProjectID: "p1"},
		{ID: "j3", Agent: "gone"},
		{ID: "j4", Agent: "helper", Project: "codes", ProjectID: "p1"}, // by name: no agent ID
		{ID: "j5", Agent: "reviewer", AgentID: "a3", Project: "app", ProjectID: "p2"},
		{ID: "j6", Agent: "coder", AgentID: "a1", Project: "old", ProjectID: "p9"}, // removed
	}
	type want struct {
		title string
		ids   []string
	}
	check := func(by string, w []want) {
		t.Helper()
		gs := groupTasks(tasks, agents, projects, by)
		if len(gs) != len(w) {
			t.Fatalf("%s: %d groups, want %d: %+v", by, len(gs), len(w), gs)
		}
		for i, g := range gs {
			var ids []string
			for _, j := range g.tasks {
				ids = append(ids, j.ID)
			}
			if g.title != w[i].title || len(ids) != len(w[i].ids) {
				t.Fatalf("%s: group %d = %q %v, want %q %v", by, i, g.title, ids, w[i].title, w[i].ids)
			}
			for k := range ids {
				if ids[k] != w[i].ids[k] {
					t.Fatalf("%s: group %d = %v, want %v", by, i, ids, w[i].ids)
				}
			}
		}
	}
	check("project", []want{{"app", []string{"j1", "j5"}}, {"codes", []string{"j2", "j4"}}, {"No project", []string{"j3"}}, {"old", []string{"j6"}}})
	check("agent", []want{{"reviewer", []string{"j1", "j5"}}, {"coder", []string{"j2", "j6"}}, {"gone", []string{"j3"}}, {"helper", []string{"j4"}}})
	check("none", []want{{"", []string{"j1", "j2", "j3", "j4", "j5", "j6"}}})

	if g := groupOf(tasks[0], agents, projects, "project"); g.tip != "~/src/app on box" {
		t.Fatalf("tip = %q", g.tip)
	}
	order := sidebarTasks(groupTasks(tasks, agents, projects, "project"))
	if order[1].ID != "j5" {
		t.Fatalf("sidebar order = %v", order)
	}
	if loadPrefs("").TaskGroups != defaultGrouping {
		t.Fatal("the default grouping is not set")
	}
}
