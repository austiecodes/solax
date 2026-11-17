package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/austiecodes/solax/internal/locale"
)

func inChinese(t *testing.T) {
	t.Helper()
	locale.Set(locale.Chinese)
	t.Cleanup(func() { locale.Set(locale.English) })
}

// In Chinese, the help says cobra's headings, the commands and the flags
// in Chinese; the names of commands and flags stay.
func TestHelpInChinese(t *testing.T) {
	inChinese(t)
	out, err := run(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"用法：", "配置：", "服务器命令：", "输出 JSON", "查看任意命令的帮助", `运行 "solax [命令] --help"`, "agent"} {
		if !strings.Contains(out, want) {
			t.Errorf("help has no %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Usage:") || strings.Contains(out, "Help about any command") {
		t.Errorf("help has English left:\n%s", out)
	}
	out, err = run(t, "agent", "discover", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"示例：", "全局选项：", "--no-super", "不把其中任何一个设为超级 Agent", "（默认 \"~\"）", "discover 的帮助"} {
		if !strings.Contains(out, want) {
			t.Errorf("discover help has no %q:\n%s", want, out)
		}
	}
}

func TestCobraErrorInChinese(t *testing.T) {
	_, err := run(t, "nope")
	if err == nil || cobraError(err) != err.Error() {
		t.Fatalf("English error changed: %v", err)
	}
	inChinese(t)
	_, err = run(t, "nope")
	if got := cobraError(err); !strings.HasPrefix(got, `"solax" 没有命令 "nope"`) {
		t.Fatalf("cobraError = %q", got)
	}
	if got := cobraError(errors.New("server said no")); got != "server said no" {
		t.Fatalf("other error = %q", got)
	}
}

// The table lines up columns of Chinese text by the columns a terminal
// draws.
func TestTableWideText(t *testing.T) {
	var b bytes.Buffer
	tw := table(&b)
	tw.Write([]byte("名称\t机器\t描述\n"))
	tw.Write([]byte("claude\tmac\t审查代码\n"))
	tw.Write([]byte("x\t工作站\t\n"))
	if err := tw.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "名称    机器    描述\nclaude  mac     审查代码\nx       工作站  \n"
	if b.String() != want {
		t.Fatalf("table =\n%q\nwant\n%q", b.String(), want)
	}
}
