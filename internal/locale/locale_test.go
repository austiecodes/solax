package locale

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// verbs are the verbs of a format, in the order of their arguments: an
// explicit index ("%[2]s") puts a verb in its place.
func verbs(format string) []string {
	re := regexp.MustCompile(`%(?:\[(\d+)\])?[-+# 0-9.]*([a-zA-Z%])`)
	var out []string
	next := 0
	for _, m := range re.FindAllStringSubmatch(format, -1) {
		if m[2] == "%" {
			continue
		}
		i := next
		if m[1] != "" {
			i, _ = strconv.Atoi(m[1])
			i--
		}
		for len(out) <= i {
			out = append(out, "")
		}
		out[i] = m[2]
		next = i + 1
	}
	return out
}

// A translation takes the arguments of its English format, of the same
// kinds, and keeps a message's line breaks at its ends.
func TestTranslationsKeepVerbs(t *testing.T) {
	for en, zh := range zhHans {
		if !slices.Equal(verbs(en), verbs(zh)) {
			t.Errorf("%q: verbs %v, translation %q has %v", en, verbs(en), zh, verbs(zh))
		}
		if strings.HasSuffix(en, "\n") != strings.HasSuffix(zh, "\n") || strings.HasPrefix(en, "\n") != strings.HasPrefix(zh, "\n") {
			t.Errorf("%q: the translation %q breaks its lines at other ends", en, zh)
		}
	}
}

// Each message that the CLI says, as a literal given to T or Tf, or in
// the help template, has a translation.
func TestCLIMessagesTranslated(t *testing.T) {
	dirs := []string{"../cli", "../localconfig"}
	tmpl := regexp.MustCompile(`\{\{Tf? "((?:[^"\\]|\\.)*)"`)
	n := 0
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no Go files in %s: %v", dir, err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			check := func(msg string) {
				n++
				if !Has(msg) {
					t.Errorf("%s: no translation of %q", filepath.Base(f), msg)
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				switch x := node.(type) {
				case *ast.CallExpr:
					if len(x.Args) > 0 && isT(x.Fun) {
						if lit, ok := x.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							s, _ := strconv.Unquote(lit.Value)
							check(s)
						}
					}
				case *ast.BasicLit:
					if x.Kind == token.STRING {
						s, _ := strconv.Unquote(x.Value)
						for _, m := range tmpl.FindAllStringSubmatch(s, -1) {
							msg, _ := strconv.Unquote(`"` + m[1] + `"`)
							check(msg)
						}
					}
				}
				return true
			})
		}
	}
	if n < 100 {
		t.Fatalf("found only %d messages: the test does not see the CLI", n)
	}
}

func isT(fn ast.Expr) bool {
	switch f := fn.(type) {
	case *ast.Ident:
		return f.Name == "T" || f.Name == "Tf"
	case *ast.SelectorExpr:
		x, ok := f.X.(*ast.Ident)
		return ok && x.Name == "locale" && (f.Sel.Name == "T" || f.Sel.Name == "Tf")
	}
	return false
}

func TestMatch(t *testing.T) {
	for in, want := range map[string]string{
		"zh_CN.UTF-8": Chinese, "zh-Hans": Chinese, "zh_TW": Chinese, "ZH": Chinese, "zh-Hant-HK": Chinese,
		"en_US.UTF-8": English, "C": English, "POSIX": English, "de_DE@euro": English,
		"": "", " auto ": "", "system": "",
	} {
		if got := Match(in); got != want {
			t.Errorf("Match(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]bool{"en": true, "en_GB": true, "zh-Hans": true, "auto": true, "fr": false, "english": false} {
		if got := Valid(in); got != want {
			t.Errorf("Valid(%q) = %t, want %t", in, got, want)
		}
	}
}

// $SOLAX_LANG, then the config, then the first locale variable that is
// set: LC_ALL=C is English even with LANG=zh_CN.
func TestDetect(t *testing.T) {
	for _, tc := range []struct {
		env        map[string]string
		configured string
		want       string
	}{
		{map[string]string{"LANG": "zh_CN.UTF-8"}, "", Chinese},
		{map[string]string{"LANG": "zh_CN.UTF-8", "LC_ALL": "C"}, "", English},
		{map[string]string{"LANG": "en_US.UTF-8"}, "zh-Hans", Chinese},
		{map[string]string{"LANG": "zh_CN.UTF-8", EnvLang: "en"}, "zh-Hans", English},
		{map[string]string{EnvLang: "auto", "LC_MESSAGES": "zh_CN"}, "", Chinese},
		{nil, "", English},
	} {
		for _, k := range []string{EnvLang, "LC_ALL", "LC_MESSAGES", "LANG"} {
			t.Setenv(k, tc.env[k])
		}
		if got := Detect(tc.configured); got != tc.want {
			t.Errorf("Detect(%q) with %v = %q, want %q", tc.configured, tc.env, got, tc.want)
		}
	}
}

func TestTAndBool(t *testing.T) {
	t.Cleanup(func() { Set(English) })
	Set(English)
	if T("running") != "running" || Bool(true) != "true" {
		t.Fatal("English is not the English text")
	}
	Set(Chinese)
	if T("running") != "运行中" || Bool(false) != "否" || T("no such message") != "no such message" {
		t.Fatalf("Chinese: %q %q", T("running"), Bool(false))
	}
	if got := Tf("unknown command \"%s\" for \"%s\"", "x", "solax"); got != `"solax" 没有命令 "x"` {
		t.Fatalf("reordered arguments: %q", got)
	}
	Set("fr")
	if Lang() != English {
		t.Fatalf("Set(fr) = %q", Lang())
	}
}

func TestWidth(t *testing.T) {
	for s, want := range map[string]int{"": 0, "abc": 3, "名称": 4, "Agent 名称": 10, "（": 2, "·": 1} {
		if got := Width(s); got != want {
			t.Errorf("Width(%q) = %d, want %d", s, got, want)
		}
	}
	if got := Pad("名称", 6); got != "名称  " {
		t.Errorf("Pad = %q", got)
	}
}
