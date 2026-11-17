package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// readInstructions runs a command with the instructions flags and returns
// what they read.
func readInstructions(t *testing.T, stdin string, args ...string) (string, bool, error) {
	t.Helper()
	var (
		fl   instructionsFlags
		text string
		ok   bool
		err  error
	)
	cmd := &cobra.Command{Use: "x", RunE: func(cmd *cobra.Command, _ []string) error {
		text, ok, err = fl.read(cmd)
		return nil
	}}
	fl.add(cmd)
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	if e := cmd.Execute(); e != nil {
		return "", false, e
	}
	return text, ok, err
}

func TestInstructionsFlags(t *testing.T) {
	f := filepath.Join(t.TempDir(), "review.md")
	if err := os.WriteFile(f, []byte("Review Go code.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, stdin string
		args        []string
		want        string
		ok          bool
	}{
		{"none", "", nil, "", false},
		{"text", "", []string{"--instructions", "Be brief."}, "Be brief.", true},
		{"empty text clears", "", []string{"--instructions", ""}, "", true},
		{"file", "", []string{"--instructions-file", f}, "Review Go code.\n", true},
		{"stdin", "From stdin.", []string{"--instructions-file", "-"}, "From stdin.", true},
	} {
		got, ok, err := readInstructions(t, tc.stdin, tc.args...)
		if err != nil || got != tc.want || ok != tc.ok {
			t.Errorf("%s: got %q, %v, %v; want %q, %v", tc.name, got, ok, err, tc.want, tc.ok)
		}
	}
	if _, _, err := readInstructions(t, "", "--instructions", "a", "--instructions-file", f); err == nil {
		t.Error("both flags: want an error")
	}
	if _, _, err := readInstructions(t, "", "--instructions-file", filepath.Join(t.TempDir(), "none.md")); err == nil {
		t.Error("missing file: want an error")
	}
}
