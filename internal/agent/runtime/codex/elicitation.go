package codex

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// elicitation is the params of mcpServer/elicitation/request.
type elicitation struct {
	ServerName string `json:"serverName"`
	Mode       string `json:"mode"` // form, openai/form, openaiForm or url
	Message    string `json:"message"`
	Meta       struct {
		// Persist names the approval scopes that Codex offers for an MCP
		// tool call: "session", "always", or a list of them.
		Persist json.RawMessage `json:"persist"`
	} `json:"_meta"`
	Schema struct {
		Properties map[string]field `json:"properties"`
		Required   []string         `json:"required"`
	} `json:"requestedSchema"`
}

// field is one primitive field of an elicitation form.
type field struct {
	Default json.RawMessage `json:"default"`
	Enum    []string        `json:"enum"`
	OneOf   []struct {
		Const string `json:"const"`
	} `json:"oneOf"`
}

// approving matches a choice that says yes, such as "Allow" or
// "Accept once".
var approving = regexp.MustCompile(`(?i)^\s*(allow|accept|approve|yes)\b`)

// answerElicitation accepts an MCP elicitation and fills each form field
// with its approving choice or its default. An MCP tool call approval
// from Codex has no fields; it is accepted for the session when Codex
// offers that. A URL needs a browser, and a required field with no
// approving choice and no default needs a person: such a request is
// cancelled, and why says so. Cancel, not decline: Codex can keep a
// decline as the user's refusal.
func answerElicitation(params json.RawMessage) (res map[string]any, why string) {
	var e elicitation
	if err := json.Unmarshal(params, &e); err != nil {
		return cancelled(), fmt.Sprintf("cancelled an MCP elicitation that solax cannot read: %v", err)
	}
	if e.Mode == "url" {
		return cancelled(), fmt.Sprintf("cancelled an MCP elicitation from %s: it needs a browser: %s", e.ServerName, e.Message)
	}
	content := map[string]any{}
	for name, f := range e.Schema.Properties {
		if v, ok := f.choice(); ok {
			content[name] = v
		}
	}
	for _, name := range e.Schema.Required {
		if _, ok := content[name]; !ok {
			return cancelled(), fmt.Sprintf("cancelled an MCP elicitation from %s: field %q needs a person: %s", e.ServerName, name, e.Message)
		}
	}
	res = map[string]any{"action": "accept", "content": content}
	if offers(e.Meta.Persist, "session") {
		res["_meta"] = map[string]any{"persist": "session"}
	}
	return res, ""
}

func cancelled() map[string]any { return map[string]any{"action": "cancel"} }

// choice returns the approving option of f, else its default.
func (f field) choice() (any, bool) {
	opts := f.Enum
	for _, o := range f.OneOf {
		opts = append(opts, o.Const)
	}
	for _, o := range opts {
		if approving.MatchString(o) {
			return o, true
		}
	}
	if len(f.Default) == 0 || string(f.Default) == "null" {
		return nil, false
	}
	return f.Default, true
}

// offers reports whether persist, a string or a list of strings, has scope.
func offers(persist json.RawMessage, scope string) bool {
	var one string
	if json.Unmarshal(persist, &one) == nil {
		return one == scope
	}
	var many []string
	_ = json.Unmarshal(persist, &many)
	for _, s := range many {
		if s == scope {
			return true
		}
	}
	return false
}
