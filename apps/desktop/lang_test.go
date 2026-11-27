package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"github.com/austiecodes/solax/apps/desktop/locales"
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
	for _, id := range []string{"settings.open", "palette.open", "nav.chat", "nav.tasks", "nav.workflows", "nav.agents", "nav.projects", "nav.machines", "sidebar.toggle", "theme.toggle", "data.reload"} {
		if _, ok := actionTitles[id]; !ok {
			t.Errorf("action %q has no title", id)
		}
	}
}

// In Chinese, the pages say their words in Chinese.
func TestPagesInChinese(t *testing.T) {
	inLocale(t, "zh-Hans")
	for page, want := range map[string][]string{
		pageChat:      {"Solax 尚未设置", "任务", "机器"},
		pageTasks:     {"还没有任务", "给 Solax 发消息"},
		pageWorkflows: {"还没有工作流", "工作流"},
		pageAgents:    {"还没有 Agent。在下方添加一个。", "添加 Agent"},
		pageProjects:  {"还没有项目。在下方添加一个。", "添加项目", "目录"},
		pageMachines:  {"审批机器", "刷新"},
		pageSettings:  {"外观", "语言", "跟随系统", "保存并连接"},
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
			for _, en := range []string{"Tasks", "Machines", "Settings", "Refresh"} {
				if tt.HasText(en) {
					t.Errorf("English %q left in %q", en, tt.Texts())
				}
			}
		})
	}
	if got := seconds(125e9); got != "2 分 5 秒" {
		t.Errorf("seconds = %q", got)
	}
	if title("nav.tasks") != "任务" || menuTitle("View") != "显示" || stateWord("running") != "运行中" {
		t.Errorf("titles: %q %q %q", title("nav.tasks"), menuTitle("View"), stateWord("running"))
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

// The upstream page kit, agent details and task grouping stay localized
// when they contain data, not just in their empty states.
func TestPopulatedPagesInChinese(t *testing.T) {
	inLocale(t, "zh-Hans")
	for _, tc := range []struct {
		name  string
		setup func(*desktop)
		want  []string
	}{
		{"tasks", func(d *desktop) { d.page = pageTasks }, []string{"所有任务", "3 个任务，1 个运行中", "最近的任务", "任务分组"}},
		{"agent", func(d *desktop) { d.page = pageAgents }, []string{"Agent 列表", "状态", "在线", "运行时默认值", "设为 Solax", "指令"}},
		{"new-agent", func(d *desktop) { d.page, d.form.open = pageAgents, true }, []string{"新建 Agent", "名称", "运行时", "描述", "指令", "添加 Agent"}},
		{"workflows", func(d *desktop) { d.page = pageWorkflows }, []string{"运行记录", "已保存", "提问", "等待中"}},
		{"workflow-run", func(d *desktop) { d.page, d.wf.sel, d.wf.item = pageWorkflows, "wfl_1", sampleRun(time.Now()) }, []string{"工作流提问", "进度", "图", "轨道", "2/3 轮", "日志", "脚本", "取消工作流"}},
		{"projects", func(d *desktop) { d.page = pageProjects }, []string{"项目", "状态", "在线", "目录", "编辑", "删除"}},
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
	if got := groupOf(protocol.Task{}, nil, nil, "project"); got.title != "无项目" || got.key != "project:" {
		t.Fatalf("no-project task group = %+v", got)
	}
	if got := suggestions[0].text(); got != "让 coder 给仓库加一个 README" {
		t.Fatalf("suggestion = %q", got)
	}
}
