// Package cli is the "solax" command. One binary has two roles:
//
//   - Client commands talk only to the server (REST).
//   - "solax daemon ..." commands control the local daemon through its
//     control socket. "solax daemon run" is the daemon itself.
//
// Agents in sessions use the client commands. The daemon sets SOLAX_*
// environment values for them.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/internal/localconfig"
	"github.com/austiecodes/solax/pkg/client"
)

// Environment values that the daemon sets for each agent session.
const (
	EnvServer    = "SOLAX_SERVER"
	EnvToken     = "SOLAX_TOKEN"
	EnvWorkspace = "SOLAX_WORKSPACE"
	EnvTask      = "SOLAX_TASK"
	EnvAgent     = "SOLAX_AGENT"
)

var errNotImplemented = errors.New("not implemented")

type globals struct {
	server string
	token  string
	json   bool
}

// NewRoot returns the root command.
func NewRoot() *cobra.Command {
	g := &globals{}
	root := &cobra.Command{
		Use:           "solax",
		Short:         "Solax: one Super Agent over coding agents on many machines",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	pf := root.PersistentFlags()
	pf.StringVar(&g.server, "server", "", "server URL (default $"+EnvServer+", then backend.url)")
	pf.StringVar(&g.token, "token", "", "token (default $"+EnvToken+", then the client token)")
	pf.BoolVar(&g.json, "json", false, "print JSON")

	root.AddGroup(
		&cobra.Group{ID: "config", Title: "Configuration:"},
		&cobra.Group{ID: "daemon", Title: "Local daemon:"},
		&cobra.Group{ID: "client", Title: "Server commands:"},
	)
	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add("config", setCmd(g), configCmd(g))
	add("daemon", daemonCmd(g), tryCmd(g))
	add("client", chatCmd(g), agentCmd(g), jobCmd(g), watchCmd(g), machineCmd(g), recallCmd(g))
	// Workspaces and team tasks come back with agent teams. Hidden until then.
	for _, c := range []*cobra.Command{wsCmd(g), taskCmd(g)} {
		c.Hidden = true
		root.AddCommand(c)
	}
	return root
}

// Execute runs the root command and exits on error.
func Execute() {
	if err := NewRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "solax:", err)
		os.Exit(1)
	}
}

// paths returns the local state paths.
func paths() (localconfig.Paths, error) {
	dir, err := localconfig.Home()
	return localconfig.Paths{Dir: dir}, err
}

// client resolves the server and the token: flag, then environment, then
// the local files. In an agent session the environment holds the agent
// token, so it wins over the client token of the machine.
func (g *globals) client() (*client.Client, error) {
	server := first(g.server, os.Getenv(EnvServer))
	token := first(g.token, os.Getenv(EnvToken))
	if server == "" || token == "" {
		p, err := paths()
		if err != nil {
			return nil, err
		}
		cfg, err := p.Load()
		if err != nil {
			return nil, err
		}
		creds, err := p.LoadCredentials()
		if err != nil {
			return nil, err
		}
		server = first(server, cfg.Backend.URL)
		token = first(token, creds.ClientToken)
	}
	if server == "" || token == "" {
		return nil, fmt.Errorf(`no backend or token: run "solax set backend --url URL --token-stdin"`)
	}
	return client.New(server, token), nil
}

// print writes v as JSON with --json, or with the text function.
func (g *globals) print(w io.Writer, v any, text func(io.Writer)) error {
	if g.json {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	text(w)
	return nil
}

// stub returns a command that has no body yet.
func stub(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE:  func(cmd *cobra.Command, args []string) error { return errNotImplemented },
	}
}

func first(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
