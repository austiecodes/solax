package runtime

import (
	"testing"

	"github.com/austiecodes/solax/internal/agent/agenttest"
	"github.com/austiecodes/solax/internal/agent/runtime/claude"
	"github.com/austiecodes/solax/internal/agent/runtime/codex"
	"github.com/austiecodes/solax/internal/agent/runtime/pi"
	"github.com/austiecodes/solax/pkg/ir"
)

func TestTurnAnswer(t *testing.T) {
	const codexStart = `{"method":"turn/started","params":{"turn":{"id":"t"}}}`
	const codexDone = `{"method":"turn/completed","params":{"turn":{"id":"t","status":"completed","items":[]}}}`
	cases := []struct {
		name    string
		decoder agenttest.Decoder
		records []string
		want    string
	}{
		{"pi tool step", pi.NewDecoder(), []string{
			`{"type":"agent_start"}`,
			`{"type":"turn_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"checking"},{"type":"toolCall"}]}}`,
			`{"type":"agent_settled"}`,
		}, ""},
		{"pi final text blocks", pi.NewDecoder(), []string{
			`{"type":"agent_start"}`,
			`{"type":"turn_end","message":{"role":"assistant","stopReason":"toolUse","content":[{"type":"text","text":"checking"}]}}`,
			`{"type":"turn_start"}`,
			`{"type":"turn_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"first"},{"type":"thinking","thinking":"private"},{"type":"text","text":"second"}]}}`,
			`{"type":"agent_settled"}`,
		}, "first\nsecond"},
		{"claude empty result", claude.NewDecoder(), []string{
			`{"type":"assistant","message":{"id":"m","content":[{"type":"text","text":"checking"},{"type":"tool_use","id":"c","name":"Read"}]}}`,
			`{"type":"result","subtype":"success","result":""}`,
		}, ""},
		{"claude tool stop", claude.NewDecoder(), []string{
			`{"type":"result","subtype":"success","stop_reason":"tool_use","result":"checking"}`,
		}, ""},
		{"codex commentary", codex.NewDecoder(), []string{
			codexStart,
			`{"method":"item/completed","params":{"turnId":"t","item":{"type":"agentMessage","id":"m","phase":"commentary","text":"checking"}}}`,
			codexDone,
		}, ""},
		{"codex unphased text before tool", codex.NewDecoder(), []string{
			codexStart,
			`{"method":"item/completed","params":{"turnId":"t","item":{"type":"agentMessage","id":"m","text":"checking"}}}`,
			`{"method":"item/completed","params":{"turnId":"t","item":{"type":"commandExecution","id":"c"}}}`,
			codexDone,
		}, ""},
		{"codex unphased terminal text", codex.NewDecoder(), []string{
			codexStart,
			`{"method":"item/completed","params":{"turnId":"t","item":{"type":"agentMessage","id":"m","text":"done"}}}`,
			codexDone,
		}, "done"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var completed []*ir.Turn
			for _, record := range tc.records {
				events, err := tc.decoder.Decode([]byte(record))
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range events {
					if e.Type == ir.TurnCompleted {
						completed = append(completed, e.Turn)
					}
				}
			}
			if len(completed) != 1 || completed[0].Answer == nil {
				t.Fatalf("want one completion with an explicit answer, got %+v", completed)
			}
			if got := *completed[0].Answer; got != tc.want {
				t.Errorf("answer = %q, want %q", got, tc.want)
			}
		})
	}
}
