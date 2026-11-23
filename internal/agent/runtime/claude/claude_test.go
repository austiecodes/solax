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
// args.txt and has --permission-prompts in its help only when prompts.
func fakeCLI(t *testing.T, prompts bool) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	help := "  --permission-mode <mode>"
	if prompts {
		help += "\n  --permission-prompts <target>"
	}
	script := "#!/bin/sh\n" +
		"case \"$1\" in --version) echo '9.9.9 (fake)'; exit 0;; --help) printf '%s\\n' '" + help + "'; exit 0;; esac\n" +
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

// A fork without --permission-prompts runs without it; print mode denies
// what would prompt anyway.
func TestPermissionPromptsOnlyWhenSupported(t *testing.T) {
	for _, prompts := range []bool{true, false} {
		bin, argsFile := fakeCLI(t, prompts)
		d := New(Config{Kind: "fakecc", Binary: bin})
		p, err := d.Probe(context.Background())
		if err != nil || !p.OK || p.Version != "9.9.9 (fake)" {
			t.Fatalf("probe = %+v, %v", p, err)
		}
		args := startArgs(t, d, argsFile)
		if got := strings.Contains(args, "--permission-prompts none"); got != prompts {
			t.Errorf("prompts=%t: args %q", prompts, args)
		}
		if !strings.Contains(args, "--permission-mode auto") {
			t.Errorf("args %q have no --permission-mode", args)
		}
	}
}

// The prompt goes in a file, unless the runtime passes it inline, as a
// fork that adds its own --append-system-prompt needs.
func TestPromptFileOrInline(t *testing.T) {
	for _, inline := range []bool{false, true} {
		bin, argsFile := fakeCLI(t, true)
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
	bin, argsFile := fakeCLI(t, true)
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
