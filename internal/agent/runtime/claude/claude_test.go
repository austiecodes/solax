package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/pkg/ir"
)

// fakeCLI writes a Claude Code stand-in that writes its arguments to
// args.txt.
func fakeCLI(t *testing.T) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\n" +
		"case \"$1\" in --version) echo '9.9.9 (fake)'; exit 0;; esac\n" +
		"echo \"$@\" > " + argsFile + "\n"
	bin = filepath.Join(dir, "fakecc")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, argsFile
}

func startArgs(t *testing.T, d *Runtime, argsFile string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := d.Start(ctx, agent.SessionSpec{SessionID: "ses_1", Dir: t.TempDir(), Instructions: "be brief"})
	if err != nil {
		t.Fatal(err)
	}
	for range s.Events() { // until the process exits
	}
	if s.Ref().Kind != d.Kind() {
		t.Fatalf("ref kind = %q, want %q", s.Ref().Kind, d.Kind())
	}
	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The session has no person to approve tool calls. It asks for bypass,
// falls back to default mode, and gets each permission request on stdout.
func TestPermissionArgs(t *testing.T) {
	for _, root := range []bool{false, true} {
		euid := 1000
		if root {
			euid = 0
		}
		geteuid = func() int { return euid }
		t.Setenv("IS_SANDBOX", "")
		t.Setenv("CLAUDE_CODE_BUBBLEWRAP", "")
		bin, argsFile := fakeCLI(t)
		d := New(Config{Kind: "fakecc", Binary: bin})
		p, err := d.Probe(context.Background())
		if err != nil || !p.OK || p.Version != "9.9.9 (fake)" {
			t.Fatalf("probe = %+v, %v", p, err)
		}
		args := startArgs(t, d, argsFile)
		if got := strings.Contains(args, "--dangerously-skip-permissions"); got == root {
			t.Errorf("root=%t: args %q", root, args)
		}
		if !strings.Contains(args, "--permission-mode default --permission-prompt-tool stdio") {
			t.Errorf("root=%t: args %q have no default mode and stdio prompts", root, args)
		}
	}
	geteuid = os.Geteuid
}

// Claude Code refuses bypass as root, unless IS_SANDBOX is 1 or
// CLAUDE_CODE_BUBBLEWRAP is set.
func TestBypassAllowed(t *testing.T) {
	defer func() { geteuid = os.Geteuid }()
	tests := []struct {
		euid int
		env  map[string]string
		want bool
	}{
		{1000, nil, true},
		{-1, nil, true}, // Windows
		{0, nil, false},
		{0, map[string]string{"IS_SANDBOX": "1"}, true},
		{0, map[string]string{"IS_SANDBOX": "true"}, false},
		{0, map[string]string{"CLAUDE_CODE_BUBBLEWRAP": "True"}, true},
		{0, map[string]string{"CLAUDE_CODE_BUBBLEWRAP": "0"}, false},
	}
	t.Setenv("IS_SANDBOX", "")
	t.Setenv("CLAUDE_CODE_BUBBLEWRAP", "")
	for _, tt := range tests {
		geteuid = func() int { return tt.euid }
		if got := bypassAllowed(tt.env); got != tt.want {
			t.Errorf("euid %d, env %v: got %t, want %t", tt.euid, tt.env, got, tt.want)
		}
	}
}

// scriptCLI writes a Claude Code stand-in that runs body after it gets
// its arguments.
func scriptCLI(t *testing.T, body string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fakecc")
	script := "#!/bin/sh\ncase \"$1\" in --version) echo '9.9.9 (fake)'; exit 0;; esac\n" + body
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// run starts a session on bin and returns its events, until it exits.
func run(t *testing.T, bin string) []ir.Event {
	t.Helper()
	d := New(Config{Kind: "fakecc", Binary: bin, StateDir: t.TempDir()})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := d.Start(ctx, agent.SessionSpec{SessionID: "ses_1", Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var evs []ir.Event
	for e := range s.Events() {
		evs = append(evs, e)
	}
	return evs
}

// A permission request is approved with its input unchanged; another
// control request gets an error, so Claude Code does not wait.
func TestAnswerControlRequests(t *testing.T) {
	out := filepath.Join(t.TempDir(), "answers.jsonl")
	run(t, scriptCLI(t, ""+
		`echo '{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":"a.txt"}}}'`+"\n"+
		`echo '{"type":"control_request","request_id":"r2","request":{"subtype":"elicitation"}}'`+"\n"+
		"read -r a; read -r b; printf '%s\\n%s\\n' \"$a\" \"$b\" > "+out+"\n"))
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	want := []string{
		`{"response":{"request_id":"r1","response":{"behavior":"allow","updatedInput":{"file_path":"a.txt"}},"subtype":"success"},"type":"control_response"}`,
		`{"response":{"error":"solax: unsupported control request elicitation","request_id":"r2","subtype":"error"},"type":"control_response"}`,
	}
	if len(lines) != len(want) {
		t.Fatalf("answers = %q, want %d lines", lines, len(want))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("answer %d = %s, want %s", i, lines[i], want[i])
		}
	}
}

// When Claude Code still refuses bypass as root, the exit says what to do.
func TestRootRefusal(t *testing.T) {
	evs := run(t, scriptCLI(t, "echo '"+rootRefusal+" for security reasons' >&2\nexit 1\n"))
	last := evs[len(evs)-1]
	if last.Type != ir.SessionExitedEvent || !strings.Contains(last.Session.Reason, "IS_SANDBOX=1") {
		t.Fatalf("last event = %+v, want an exit that names IS_SANDBOX=1", last.Session)
	}
}

// The prompt goes in a file, unless the runtime passes it inline, as a
// fork that adds its own --append-system-prompt needs.
func TestPromptFileOrInline(t *testing.T) {
	for _, inline := range []bool{false, true} {
		bin, argsFile := fakeCLI(t)
		d := New(Config{Kind: "fakecc", Binary: bin, StateDir: t.TempDir(), InlinePrompt: inline})
		args := startArgs(t, d, argsFile)
		if got := strings.Contains(args, "--append-system-prompt-file "); got == inline {
			t.Errorf("inline=%t: args %q", inline, args)
		}
		if inline && !strings.Contains(args, "--append-system-prompt be brief") {
			t.Errorf("inline: args %q", args)
		}
	}
}

func TestKindDefaults(t *testing.T) {
	if d := New(Config{}); d.Kind() != ir.KindClaude || d.cfg.Binary != "claude" {
		t.Fatalf("default = %+v", d.cfg)
	}
	if d := New(Config{Kind: "mycc"}); d.cfg.Binary != "mycc" {
		t.Fatalf("mycc binary = %q", d.cfg.Binary)
	}
}

// A session with a plugin starts with --plugin-dir.
func TestPluginDir(t *testing.T) {
	bin, argsFile := fakeCLI(t)
	d := New(Config{Kind: "fakecc", Binary: bin, StateDir: t.TempDir()})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := d.Start(ctx, agent.SessionSpec{SessionID: "ses_1", Dir: t.TempDir(), Plugin: "/assets/plugins/claudecode"})
	if err != nil {
		t.Fatal(err)
	}
	for range s.Events() {
	}
	b, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(b), "--plugin-dir /assets/plugins/claudecode") {
		t.Fatalf("args %q have no --plugin-dir", b)
	}
}
