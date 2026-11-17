// Package locale says the messages of the solax CLI in the language of
// its user: English, the language the code is written in, or Simplified
// Chinese. A message is its English text; T looks up its translation, and
// a message without one stays English.
package locale

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

// The languages of the CLI.
const (
	English = "en"
	Chinese = "zh-Hans"
)

// EnvLang chooses the language: "en", "zh-Hans" (or "zh", "zh_CN"...).
// It wins over config.yaml, so the daemon can run agent sessions in
// English, the language of their prompts.
const EnvLang = "SOLAX_LANG"

var current atomic.Pointer[string]

// Lang returns the language in use.
func Lang() string {
	if l := current.Load(); l != nil {
		return *l
	}
	return English
}

// Set chooses the language in use; "" is English.
func Set(lang string) {
	if lang != Chinese {
		lang = English
	}
	current.Store(&lang)
}

// Match returns the language of the CLI for a tag of a language or a
// locale ("zh_CN.UTF-8", "zh-Hant", "en_US", "C"): Chinese for Chinese,
// English for any other, "" for none ("", "auto", "system").
func Match(tag string) string {
	t := strings.ToLower(strings.TrimSpace(tag))
	t, _, _ = strings.Cut(t, ".") // zh_CN.UTF-8
	t, _, _ = strings.Cut(t, "@") // de_DE@euro
	t = strings.ReplaceAll(t, "_", "-")
	switch {
	case t == "" || t == "auto" || t == "system":
		return ""
	case t == "zh" || strings.HasPrefix(t, "zh-"):
		return Chinese
	}
	return English
}

// Valid tells whether tag names a language of the CLI, or none.
func Valid(tag string) bool {
	t := Match(tag)
	if t != English {
		return true
	}
	l := strings.ToLower(strings.TrimSpace(tag))
	return l == "en" || strings.HasPrefix(l, "en-") || strings.HasPrefix(l, "en_")
}

// Detect returns the language to use: $SOLAX_LANG, then the configured
// one, then the locale of messages ($LC_ALL, $LC_MESSAGES, $LANG: the
// first that is set decides, as POSIX has it), else English.
func Detect(configured string) string {
	for _, v := range []string{os.Getenv(EnvLang), configured} {
		if l := Match(v); l != "" {
			return l
		}
	}
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			if l := Match(v); l != "" {
				return l
			}
			return English
		}
	}
	return English
}

// T returns the translation of an English message, or the message.
func T(msg string) string {
	if Lang() == Chinese {
		if s, ok := zhHans[msg]; ok {
			return s
		}
	}
	return msg
}

// Has tells whether an English message has a translation.
func Has(msg string) bool {
	_, ok := zhHans[msg]
	return ok
}

// Tf formats the translation of an English format. A translation may
// take the arguments in another order, with %[2]s.
func Tf(format string, args ...any) string {
	return fmt.Sprintf(T(format), args...)
}

// Bool says a bool: "true" or "false" in English, as %t writes it.
func Bool(b bool) string { return T(strconv.FormatBool(b)) }
