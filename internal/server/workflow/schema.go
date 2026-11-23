package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Structured answers (dynamic-workflow.md §5.2). A task with opts.schema
// asks its agent to end the answer with one JSON object; Go takes the last
// one and checks it against the schema.

// schemaPrompt is added to the prompt of a task with a schema.
func schemaPrompt(schema json.RawMessage) string {
	return "\n\nEnd your answer with one JSON object in a ```json code block. It must match this JSON Schema:\n```json\n" +
		string(schema) + "\n```"
}

// schemaRetry is the follow-up for an answer that did not match.
func schemaRetry(err error) string {
	return "Your answer did not match the JSON Schema: " + err.Error() +
		"\nAnswer again. End with one JSON object in a ```json code block that matches the schema."
}

// lastJSON returns the last JSON object in text: the last ```json block
// that holds one, or else the last {...} that parses.
func lastJSON(text string) (json.RawMessage, bool) {
	for rest := text; ; {
		i := strings.LastIndex(rest, "```json")
		if i < 0 {
			break
		}
		body := rest[i+len("```json"):]
		if end := strings.Index(body, "```"); end >= 0 {
			body = body[:end]
		}
		if obj, ok := object(strings.TrimSpace(body)); ok {
			return obj, true
		}
		rest = rest[:i]
	}
	// Top-level objects in order: after an object, go on past its end, so
	// an object inside it is not taken for the last one.
	var last json.RawMessage
	for i := 0; i < len(text); {
		j := strings.IndexByte(text[i:], '{')
		if j < 0 {
			break
		}
		i += j
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		var raw json.RawMessage
		if dec.Decode(&raw) == nil {
			if obj, ok := object(string(raw)); ok {
				last = obj
				i += int(dec.InputOffset())
				continue
			}
		}
		i++
	}
	return last, last != nil
}

func object(s string) (json.RawMessage, bool) {
	var v map[string]any
	if !strings.HasPrefix(s, "{") || json.Unmarshal([]byte(s), &v) != nil {
		return nil, false
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// checkSchemaRoot reports whether schema can be used: a JSON object with
// type "object".
func checkSchemaRoot(schema json.RawMessage) error {
	var s map[string]any
	if err := json.Unmarshal(schema, &s); err != nil {
		return fmt.Errorf("schema is not a JSON object")
	}
	if s["type"] != "object" {
		return fmt.Errorf(`the root of a schema must have "type": "object"`)
	}
	return nil
}

// validate checks v against schema. It knows the JSON Schema keywords type,
// properties, required, items, enum and additionalProperties; others are
// ignored.
func validate(schema json.RawMessage, v json.RawMessage) error {
	var s, val any
	if err := json.Unmarshal(schema, &s); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(v))
	dec.UseNumber()
	if err := dec.Decode(&val); err != nil {
		return err
	}
	return check("$", s, val)
}

func check(path string, schema, v any) error {
	s, ok := schema.(map[string]any)
	if !ok {
		return nil // true, or a form we do not know: accept
	}
	if enum, ok := s["enum"].([]any); ok && !slices.ContainsFunc(enum, func(e any) bool { return same(e, v) }) {
		return fmt.Errorf("%s: %s is not one of the enum values", path, show(v))
	}
	if t, ok := s["type"]; ok && !typeOK(t, v) {
		return fmt.Errorf("%s: want type %v, got %s", path, t, kind(v))
	}
	switch v := v.(type) {
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		if req, ok := s["required"].([]any); ok {
			for _, r := range req {
				if name, ok := r.(string); ok {
					if _, ok := v[name]; !ok {
						return fmt.Errorf("%s: missing required property %q", path, name)
					}
				}
			}
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if ps, ok := props[k]; ok {
				if err := check(path+"."+k, ps, v[k]); err != nil {
					return err
				}
				continue
			}
			if s["additionalProperties"] == false {
				return fmt.Errorf("%s: property %q is not allowed", path, k)
			}
		}
	case []any:
		if items, ok := s["items"]; ok {
			for i, e := range v {
				if err := check(fmt.Sprintf("%s[%d]", path, i), items, e); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func typeOK(t, v any) bool {
	switch t := t.(type) {
	case string:
		return isType(t, v)
	case []any:
		return slices.ContainsFunc(t, func(x any) bool { s, _ := x.(string); return isType(s, v) })
	}
	return true
}

func isType(t string, v any) bool {
	switch t {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	case "number":
		_, ok := v.(json.Number)
		return ok
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		_, err := n.Int64()
		return err == nil
	}
	return true
}

func kind(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number:
		return "number"
	case nil:
		return "null"
	}
	return fmt.Sprintf("%T", v)
}

func same(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func show(v any) string {
	b, _ := json.Marshal(v)
	if len(b) > 60 {
		return string(b[:60]) + "…"
	}
	return string(b)
}
