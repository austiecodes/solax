package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
	"github.com/austiecodes/solax/pkg/client"
	"github.com/austiecodes/solax/pkg/ir"
	"github.com/austiecodes/solax/pkg/protocol"
)

// testLocale is the language the tests show: English, unless a test says
// otherwise, whatever the languages of the machine that runs them.
var testLocale atomic.Pointer[string]

func TestMain(m *testing.M) {
	setTestLocale("en")
	i18n.SetLocaleFunc(func() string { return *testLocale.Load() })
	os.Exit(m.Run())
}

func setTestLocale(tag string) { testLocale.Store(&tag) }

func inLocale(t *testing.T, tag string) {
	t.Helper()
	setTestLocale(tag)
	t.Cleanup(func() { setTestLocale("en") })
}

// Each message has a translation, and each action and menu a title.
func TestCatalogsComplete(t *testing.T) {
	read := func(name string) map[string]any {
		b, err := os.ReadFile("locales/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	en, zh := read("en.json"), read("zh-Hans.json")
	for k := range en {
		if strings.HasPrefix(k, "@") {
			continue
		}
		if _, ok := zh[k]; !ok {
			t.Errorf("zh-Hans.json has no %q", k)
		}
	}
	if got := locales.In("zh-Hans").ToolsUsed(3); got != "使用了 3 个工具" {
		t.Errorf("ToolsUsed(3) = %q", got)
	}
	if got := locales.In("en").ToolsUsed(1); got != "1 tool used" {
		t.Errorf("ToolsUsed(1) = %q", got)
	}
	for _, id := range []string{"settings.open", "palette.open", "nav.chat", "nav.jobs", "nav.agents", "nav.machines", "sidebar.toggle", "theme.toggle", "data.reload"} {
		if _, ok := actionTitles[id]; !ok {
			t.Errorf("action %q has no title", id)
		}
	}
}

// In Chinese, the pages say their words in Chinese.
func TestPagesInChinese(t *testing.T) {
	inLocale(t, "zh-Hans")
	for page, want := range map[string][]string{
		pageChat:     {"Solax 尚未设置", "任务", "机器"},
		pageJobs:     {"还没有任务", "给 Solax 发消息"},
		pageAgents:   {"还没有 Agent。在下方添加一个。", "添加 Agent", "工作目录"},
		pageMachines: {"审批机器", "刷新"},
		pageSettings: {"外观", "语言", "跟随系统", "保存并连接"},
	} {
		t.Run(page, func(t *testing.T) {
			d := testDesktop(t)
			d.page = page
			tt := ui.NewTester(d.view, 1240, 820)
			for _, w := range want {
				if !tt.HasText(w) {
					t.Errorf("no %q in %q", w, tt.Texts())
				}
			}
			for _, en := range []string{"Jobs", "Machines", "Settings", "Refresh"} {
				if tt.HasText(en) {
					t.Errorf("English %q left in %q", en, tt.Texts())
				}
			}
		})
	}
	if got := seconds(125e9); got != "2 分 5 秒" {
		t.Errorf("seconds = %q", got)
	}
	if title("nav.jobs") != "任务" || menuTitle("View") != "显示" || stateWord("running") != "运行中" {
		t.Errorf("titles: %q %q %q", title("nav.jobs"), menuTitle("View"), stateWord("running"))
	}
}

// The Language select names each language in itself and keeps the
// choice; the Appearance select keeps working in another language.
func TestLanguageChoice(t *testing.T) {
	inLocale(t, "zh-Hans")
	d := testDesktop(t)
	d.page = pageSettings
	if got := slices.Collect(func(yield func(string) bool) {
		for _, tag := range languages() {
			if !yield(languageLabel(tag)) {
				return
			}
		}
	}); !slices.Equal(got, []string{"跟随系统", "English", "简体中文"}) {
		t.Fatalf("labels = %q", got)
	}
	d.setLanguage("zh-Hans")
	if d.lang != "zh-Hans" {
		t.Fatalf("lang = %q", d.lang)
	}
	if appearanceLabel("dark") != "深色" || appearanceLabel("") != "跟随系统" {
		t.Fatal("appearance labels are not in Chinese")
	}
}

// waitFor runs the updates of background work until done.
func waitFor(t *testing.T, d *desktop, done func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !done() {
		select {
		case fn := <-d.queue:
			fn()
		case <-deadline:
			t.Fatalf("timed out; status %q", d.status)
		}
	}
}

// fakeServer answers the requests of the Agents page: it lists machines
// and agents, and creates agents.
type fakeServer struct {
	mu       sync.Mutex
	machines []protocol.Machine
	agents   []protocol.Agent
	created  []protocol.CreateAgentRequest
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/machines":
		_ = json.NewEncoder(w).Encode(f.machines)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/agents":
		_ = json.NewEncoder(w).Encode(f.agents)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/agents":
		var req protocol.CreateAgentRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.created = append(f.created, req)
		role := ""
		if req.Super {
			role = "super"
		}
		a := protocol.Agent{ID: "agt_" + req.Name, Name: req.Name, MachineID: req.Machine, Driver: req.Driver, Dir: req.Dir, Role: role}
		f.agents = append(f.agents, a)
		_ = json.NewEncoder(w).Encode(a)
	default:
		http.NotFound(w, r)
	}
}

// The Agents page offers the CLIs that the daemon found and no agent
// uses; "Add" sets one up in the folder given, and it is offered no more.
func TestDiscoverAdds(t *testing.T) {
	srv := &fakeServer{machines: []protocol.Machine{{
		ID: "mch_1", Name: "Ada-MacBook.local", State: protocol.MachineApproved, Online: true,
		Drivers: []protocol.DriverProbe{
			{Kind: ir.KindClaude, Version: "2.1.0", OK: true},
			{Kind: ir.KindPi, Version: "0.8.0", OK: true},
			{Kind: ir.KindCodex, OK: false, Problem: "not found"},
		},
	}}}
	hs := httptest.NewServer(srv)
	defer hs.Close()

	d := testDesktop(t)
	d.queue = make(chan func(), 64)
	d.client = client.New(hs.URL, "sxc_test")
	d.page = pageAgents
	d.machines = srv.machines
	tt := ui.NewTester(d.view, 1240, 820)
	for _, want := range []string{"Found on your machines", "claude", "pi", "becomes Solax", "Add all"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q in %q", want, tt.Texts())
		}
	}
	d.discoverDir = "~/src/app"
	if err := tt.Click("Add"); err != nil { // the first: claude
		t.Fatal(err)
	}
	waitFor(t, d, func() bool { return !d.adding && len(d.agents) == 1 })
	if d.status != "" {
		t.Fatalf("status = %q", d.status)
	}
	got := srv.created[0]
	if got.Name != "claude" || got.Driver != ir.KindClaude || got.Machine != "mch_1" || got.Dir != "~/src/app" || got.Super || got.Description == "" {
		t.Fatalf("created %+v", got)
	}
	tt.Frame()
	if tt.HasText("Add all") || !tt.HasText("becomes Solax") {
		t.Fatalf("after the add: %q", tt.Texts())
	}
	if err := tt.Click("Add"); err != nil { // pi, as Solax
		t.Fatal(err)
	}
	waitFor(t, d, func() bool { return !d.adding && len(d.agents) == 2 })
	if !srv.created[1].Super || srv.created[1].Driver != ir.KindPi {
		t.Fatalf("created %+v", srv.created[1])
	}
	tt.Frame()
	if tt.HasText("Found on your machines") {
		t.Fatalf("still offered: %q", tt.Texts())
	}
}

// The upstream page kit, agent details and job grouping stay localized
// when they contain data, not just in their empty states.
func TestPopulatedPagesInChinese(t *testing.T) {
	inLocale(t, "zh-Hans")
	for _, tc := range []struct {
		name  string
		setup func(*desktop)
		want  []string
	}{
		{"jobs", func(d *desktop) { d.page = pageJobs }, []string{"所有任务", "3 个任务，1 个运行中", "最近的任务", "任务分组"}},
		{"agent", func(d *desktop) { d.page = pageAgents }, []string{"Agent 列表", "状态", "在线", "驱动默认值", "设为 Solax"}},
		{"new-agent", func(d *desktop) { d.page, d.form.open = pageAgents, true }, []string{"新建 Agent", "名称", "驱动", "描述", "添加 Agent"}},
		{"machines", func(d *desktop) { d.page = pageMachines }, []string{"配对码", "最近在线", "机器 ID", "就绪", "不可用"}},
		{"settings", func(d *desktop) { d.page = pageSettings }, []string{"外观", "语言", "侧边栏", "任务分组方式", "项目", "保存并连接"}},
		{"palette", func(d *desktop) { d.openPalette() }, []string{"搜索", "移动", "打开", "关闭"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := sampleDesktop(t)
			tc.setup(d)
			tt := ui.NewTester(d.view, 1240, 820)
			for _, want := range tc.want {
				if !tt.HasText(want) {
					t.Errorf("no %q in %q", want, tt.Texts())
				}
			}
		})
	}
}

// Selects show translated labels, but their values remain stable IDs.
func TestLocalizedSelectValues(t *testing.T) {
	inLocale(t, "zh-Hans")
	for _, tc := range []struct {
		name, initial, shown, click, want string
		values                            []string
		label                             func(string) string
	}{
		{"appearance", "", "跟随系统", "深色", "dark", appearances, appearanceLabel},
		{"language", "", "跟随系统", "简体中文", "zh-Hans", languages(), languageLabel},
		{"grouping", "project", "项目", "不分组", "none", groupingIDs(), groupingLabel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.initial
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Row(c).Width(180).Children(func() {
					if next, changed := choose(c, tc.name, value, tc.values, tc.label); changed {
						value = next
					}
				})
			}, 500, 400)
			if err := tt.Click(tc.shown); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click(tc.click); err != nil {
				t.Fatal(err)
			}
			if value != tc.want {
				t.Fatalf("value = %q, want %q", value, tc.want)
			}
		})
	}
}

// Group labels are computed at render time, so switching language does
// not change the persisted grouping or leave old-language labels behind.
func TestGroupLabelsFollowLocale(t *testing.T) {
	inLocale(t, "en")
	if got := groupingLabel("project"); got != "Project" {
		t.Fatalf("English label = %q", got)
	}
	setTestLocale("zh-Hans")
	if got := groupingLabel("project"); got != "项目" {
		t.Fatalf("Chinese label = %q", got)
	}
	if got := groupOf(protocol.Job{}, nil, "project"); got.title != "其他" || got.key != "project:" {
		t.Fatalf("orphaned job group = %+v", got)
	}
	if got := suggestions[0].text(); got != "让 coder 给仓库加一个 README" {
		t.Fatalf("suggestion = %q", got)
	}
}
