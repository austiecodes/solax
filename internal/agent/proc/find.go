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

// childEnv is env for a process of bin: its PATH gets the folder of bin,
// then the install folders that exist, at the end and only where PATH
// lacks them. A CLI often runs "#!/usr/bin/env node", and a daemon that
// launchd started has no node on its PATH: the folder of bin has it for
// an npm package of nvm, and the newest nvm folder for a CLI installed
// elsewhere, as ~/.local/bin/pi.
func childEnv(env []string, bin string) []string {
	dirs := []string{filepath.Dir(bin)}
	for _, d := range InstallDirs {
		if found := globDirs(expandHome(d)); len(found) > 0 && isDir(found[0]) {
			dirs = append(dirs, found[0]) // of nvm, the newest only
		}
	}
	return pathWith(env, dirs...)
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// pathWith returns env with dirs added at the end of PATH, in order,
// leaving out the ones PATH has. The last PATH in env counts, as os/exec
// uses the last of duplicate keys.
func pathWith(env []string, dirs ...string) []string {
	i := len(env) - 1
	for ; i >= 0 && !strings.HasPrefix(env[i], "PATH="); i-- {
	}
	var have []string
	if i >= 0 {
		have = filepath.SplitList(strings.TrimPrefix(env[i], "PATH="))
	}
	var add []string
	for _, d := range dirs {
		if d != "" && d != "." && !slices.Contains(have, d) && !slices.Contains(add, d) {
			add = append(add, d)
		}
	}
	if len(add) == 0 {
		return env
	}
	path := strings.Join(slices.Concat(have, add), string(os.PathListSeparator))
	if i < 0 {
		return append(slices.Clone(env), "PATH="+path)
	}
	out := slices.Clone(env)
	out[i] = "PATH=" + path
	return out
}
