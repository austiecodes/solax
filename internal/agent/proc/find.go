package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// InstallDirs are the folders where installers put agent CLIs. A daemon
// that launchd or a GUI app started may not have them on its PATH, so
// Find looks there too. "~" is the home directory; "*" matches each
// installed Node version of nvm, the newest first.
var InstallDirs = []string{
	"~/.local/bin",
	"~/.claude/local",
	"~/bin",
	"/opt/homebrew/bin",
	"/usr/local/bin",
	"~/.bun/bin",
	"~/.npm-global/bin",
	"~/.volta/bin",
	"~/.nvm/versions/node/*/bin",
}

// ErrNotFound means that Find found no executable.
var ErrNotFound = errors.New("not found on the PATH or in the usual install folders")

// Find returns the path of an agent CLI. A name with a slash is a path
// and is only checked. Else Find looks on the PATH, then in InstallDirs.
func Find(name string) (string, error) {
	if strings.ContainsRune(name, '/') {
		p := expandHome(name)
		if !executable(p) {
			return "", fmt.Errorf("%s: %w", name, ErrNotFound)
		}
		return p, nil
	}
	if p, err := exec.LookPath(name); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs, nil
		}
		return p, nil
	}
	for _, d := range InstallDirs {
		for _, dir := range globDirs(expandHome(d)) {
			if p := filepath.Join(dir, name); executable(p) {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("%s: %w", name, ErrNotFound)
}

// Fingerprint names the file that Find finds for name, with its size and
// time: an install, an update or a removal changes it. It starts no
// process, so the daemon can check it often.
func Fingerprint(name string) string {
	p, err := Find(name)
	if err != nil {
		return name + ":missing"
	}
	st, err := os.Stat(p)
	if err != nil {
		return p + ":missing"
	}
	return fmt.Sprintf("%s:%d:%d", p, st.Size(), st.ModTime().UnixNano())
}

func executable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0
}

// globDirs expands one "*" in a folder pattern, newest version first.
func globDirs(pattern string) []string {
	if !strings.Contains(pattern, "*") {
		return []string{pattern}
	}
	matches, _ := filepath.Glob(pattern)
	slices.SortFunc(matches, func(a, b string) int { return compareVersions(b, a) })
	return matches
}

// compareVersions compares paths that differ in a version such as
// "v22.13.0": number by number, so v24 comes after v9.
func compareVersions(a, b string) int {
	na, nb := numbers(a), numbers(b)
	for i := 0; i < min(len(na), len(nb)); i++ {
		if na[i] != nb[i] {
			return na[i] - nb[i]
		}
	}
	return len(na) - len(nb)
}

func numbers(s string) []int {
	var out []int
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r < '0' || r > '9' }) {
		n, _ := strconv.Atoi(f)
		out = append(out, n)
	}
	return out
}

// expandHome turns a leading "~/" into the home directory.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
}

// pathWith returns env with dir added at the end of PATH, unless PATH has
// it. A CLI found outside the PATH, as an npm package of nvm, often runs
// "#!/usr/bin/env node", and its node is in the same folder. The last
// PATH in env counts, as os/exec uses the last of duplicate keys.
func pathWith(env []string, dir string) []string {
	if dir == "" || dir == "." {
		return env
	}
	for i := len(env) - 1; i >= 0; i-- {
		kv := env[i]
		v, ok := strings.CutPrefix(kv, "PATH=")
		if !ok {
			continue
		}
		if slices.Contains(filepath.SplitList(v), dir) {
			return env
		}
		out := slices.Clone(env)
		out[i] = kv + string(os.PathListSeparator) + dir
		return out
	}
	return append(env, "PATH="+dir)
}
