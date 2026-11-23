package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLastJSON(t *testing.T) {
	cases := []struct {
		name, text, want string
		ok               bool
	}{
		{"fenced block", "Done.\n```json\n{\"pass\": true}\n```\n", `{"pass":true}`, true},
		{"last fenced block wins", "```json\n{\"a\":1}\n```\nthen\n```json\n{\"a\":2}\n```", `{"a":2}`, true},
		{"bare object at the end", `I checked it. {"pass": false, "issues": ["x"]}`, `{"pass":false,"issues":["x"]}`, true},
		{"nested object is not the last", `result {"a": {"b": 1}} ok`, `{"a":{"b":1}}`, true},
		{"no JSON", "LGTM", "", false},
		{"array is not an object", "```json\n[1,2]\n```", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := lastJSON(c.text)
			if ok != c.ok || string(got) != c.want {
				t.Fatalf("lastJSON = %s, %v; want %s, %v", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"pass": {"type": "boolean"},
			"issues": {"type": "array", "items": {"type": "string"}},
			"level": {"enum": ["low", "high"]},
			"count": {"type": "integer"}
		},
		"required": ["pass", "issues"],
		"additionalProperties": false
	}`)
	cases := []struct {
		v, err string
	}{
		{`{"pass": true, "issues": []}`, ""},
		{`{"pass": true, "issues": ["a"], "level": "low", "count": 3}`, ""},
		{`{"issues": []}`, `missing required property "pass"`},
		{`{"pass": "yes", "issues": []}`, "$.pass: want type boolean, got string"},
		{`{"pass": true, "issues": [1]}`, "$.issues[0]: want type string, got number"},
		{`{"pass": true, "issues": [], "level": "mid"}`, "not one of the enum values"},
		{`{"pass": true, "issues": [], "count": 1.5}`, "want type integer"},
		{`{"pass": true, "issues": [], "extra": 1}`, `property "extra" is not allowed`},
	}
	for _, c := range cases {
		err := validate(schema, json.RawMessage(c.v))
		switch {
		case c.err == "" && err != nil:
			t.Errorf("validate(%s) = %v, want nil", c.v, err)
		case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
			t.Errorf("validate(%s) = %v, want %q", c.v, err, c.err)
		}
	}
	if err := checkSchemaRoot(json.RawMessage(`{"type":"array"}`)); err == nil {
		t.Error("checkSchemaRoot of an array schema: want an error")
	}
}
