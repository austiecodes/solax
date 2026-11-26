package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/austiecodes/solax/internal/agent"
	"github.com/austiecodes/solax/pkg/ir"
)

// fakeAppServer writes a Codex stand-in that logs each request to
// requests.jsonl and answers it with thread "th_1".
func fakeAppServer(t *testing.T) (bin, reqFile string) {
	t.Helper()
	dir := t.TempDir()
	reqFile = filepath.Join(dir, "requests.jsonl")
	script := "#!/bin/sh\n" +
		"while IFS= read -r line; do\n" +
		"  printf '%s\\n' \"$line\" >> " + reqFile + "\n" +
		"  id=$(printf '%s' \"$line\" | sed -n 's/.*\"id\":\\([0-9][0-9]*\\).*/\\1/p')\n" +
		"  [ -n \"$id\" ] && printf '{\"id\":%s,\"result\":{\"thread\":{\"id\":\"th_1\"}}}\\n' \"$id\"\n" +
		"done\n"
	bin = filepath.Join(dir, "fakecodex")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, reqFile
}

// A new or resumed thread runs without a sandbox and sends every approval
// prompt to the session; none is rejected before it gets there.
func TestFullAccess(t *testing.T) {
	for _, method := range []string{"thread/start", "thread/resume"} {
		bin, reqFile := fakeAppServer(t)
		d := New(Config{Binary: bin, StateDir: t.TempDir()})
		spec := agent.SessionSpec{SessionID: "ses_1", Dir: t.TempDir()}
		if method == "thread/resume" {
			spec.Resume = &ir.SessionRef{Kind: ir.KindCodex, NativeID: "th_1"}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		s, err := d.Start(ctx, spec)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		_ = s.Close(ctx)
		cancel()
		b, err := os.ReadFile(reqFile)
		if err != nil {
			t.Fatal(err)
		}
		var req string
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, `"method":"`+method+`"`) {
				req = line
			}
		}
		if req == "" {
			t.Fatalf("no %s request in %s", method, b)
		}
		for _, want := range []string{
			`"approvalPolicy":{"granular":{"mcp_elicitations":true,"request_permissions":true,"rules":true,"sandbox_approval":true,"skill_approval":true}}`,
			`"sandbox":"danger-full-access"`,
		} {
			if !strings.Contains(req, want) {
				t.Errorf("%s = %s, want %s", method, req, want)
			}
		}
	}
}

// Every approval request is approved for the session, so a stray request
// never fails a tool call. Other requests stay unsupported.
func TestApprove(t *testing.T) {
	tests := []struct {
		method, params, want string
	}{
		{"item/commandExecution/requestApproval", `{}`, `{"decision":"acceptForSession"}`},
		{"item/fileChange/requestApproval", `{}`, `{"decision":"acceptForSession"}`},
		{"execCommandApproval", `{}`, `{"decision":"approved_for_session"}`},
		{"applyPatchApproval", `{}`, `{"decision":"approved_for_session"}`},
		{"item/permissions/requestApproval", `{"permissions":{"network":{"enabled":true}}}`, `{"permissions":{"network":{"enabled":true}},"scope":"session"}`},
		{"item/permissions/requestApproval", `{}`, `{"permissions":{},"scope":"session"}`},
	}
	for _, tt := range tests {
		res, ok := approve(tt.method, json.RawMessage(tt.params))
		if !ok {
			t.Errorf("%s: not approved", tt.method)
			continue
		}
		b, _ := json.Marshal(res)
		if string(b) != tt.want {
			t.Errorf("%s = %s, want %s", tt.method, b, tt.want)
		}
	}
	if _, ok := approve("item/tool/requestUserInput", nil); ok {
		t.Error("item/tool/requestUserInput: approved, want unsupported")
	}
}

// An elicitation is accepted with its approving choices or defaults, and
// cancelled when it needs a browser or a person.
func TestAnswerElicitation(t *testing.T) {
	tests := []struct {
		name, params, want string
		cancelled          bool
	}{
		{
			"codex tool call approval",
			`{"serverName":"github","mode":"form","message":"Allow?","_meta":{"codex_approval_kind":"mcp_tool_call","persist":["session","always"]},"requestedSchema":{"type":"object","properties":{}}}`,
			`{"_meta":{"persist":"session"},"action":"accept","content":{}}`, false,
		},
		{
			"no session scope",
			`{"serverName":"github","mode":"form","message":"Allow?","_meta":{"persist":"always"},"requestedSchema":{"type":"object","properties":{}}}`,
			`{"action":"accept","content":{}}`, false,
		},
		{
			"approving choice and default",
			`{"serverName":"s","mode":"form","message":"m","requestedSchema":{"type":"object","properties":{"answer":{"type":"string","oneOf":[{"const":"deny","title":"No"},{"const":"Allow once","title":"Yes"}]},"count":{"type":"integer","default":3}},"required":["answer"]}}`,
			`{"action":"accept","content":{"answer":"Allow once","count":3}}`, false,
		},
		{
			"required field with no choice",
			`{"serverName":"s","mode":"form","message":"Your email?","requestedSchema":{"type":"object","properties":{"email":{"type":"string"}},"required":["email"]}}`,
			`{"action":"cancel"}`, true,
		},
		{
			"url",
			`{"serverName":"s","mode":"url","message":"Sign in","url":"https://example.com","elicitationId":"e1"}`,
			`{"action":"cancel"}`, true,
		},
	}
	for _, tt := range tests {
		res, why := answerElicitation(json.RawMessage(tt.params))
		b, _ := json.Marshal(res)
		if string(b) != tt.want || (why != "") != tt.cancelled {
			t.Errorf("%s = %s (why %q), want %s", tt.name, b, why, tt.want)
		}
	}
}
