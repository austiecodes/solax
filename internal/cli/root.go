// Package cli is the "solax" command. One binary has three roles:
//
//   - Client commands talk only to the server (REST).
//   - "solax daemon ..." commands control the local daemon through its
//     control socket. "solax daemon run" is the daemon itself.
//   - "solax server ..." commands run and manage a server. "solax server
//     run" is the server itself. The server and the daemon are separate
//     processes, also when they run on the same machine.
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
	"github.com/austiecodes/solax/internal/locale"
	"github.com/austiecodes/solax/pkg/client"
)

// Environment values that the daemon sets for each agent session.
const (
	EnvServer = "SOLAX_SERVER"
	EnvToken  = "SOLAX_TOKEN"
)

type globals struct {
	server string
	token  string
	json   bool
}

// NewRoot returns the root command, in the language in use (package
// locale): Execute chooses it before it builds the commands.
func NewRoot() *cobra.Command {
	g := &globals{}
	root := &cobra.Command{
		Use:           "solax",
		Short:         T("Solax: one Super Agent over coding agents on many machines"),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	pf := root.PersistentFlags()
	pf.StringVar(&g.server, "server", "", Tf("server URL (default $%s, then backend.url)", EnvServer))
	pf.StringVar(&g.token, "token", "", Tf("token (default $%s, then the client token)", EnvToken))
	pf.BoolVar(&g.json, "json", false, T("print JSON"))

	root.AddGroup(
		&cobra.Group{ID: "config", Title: T("Configuration:")},
		&cobra.Group{ID: "daemon", Title: T("Local daemon:")},
		&cobra.Group{ID: "server", Title: T("Server:")},
		&cobra.Group{ID: "client", Title: T("Server commands:")},
	)
	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add("config", setupCmd(g), setCmd(), configCmd(g))
	add("daemon", daemonCmd(g), tryCmd(g))
	add("server", serverCmd(g))
	add("client", chatCmd(g), agentCmd(g), projectCmd(g), taskCmd(g), workflowCmd(g), watchCmd(g), machineCmd(g), recallCmd(g))
	localizeCobra(root)
	return root
}

// Execute runs the root command and exits on error.
func Execute() {
	locale.Set(locale.Detect(configuredLang()))
	if err := NewRoot().Execute(); err != nil {
		if !errors.Is(err, errSilent) {
			fmt.Fprintln(os.Stderr, "solax:", cobraError(err))
		}
		os.Exit(1)
	}
}

// configuredLang is lang of config.yaml, or "" when it cannot be read: a
// broken config must not stop "solax --help".
func configuredLang() string {
	p, err := paths()
	if err != nil {
		return ""
	}
	cfg, err := p.Load()
	if err != nil {
		return ""
	}
	return cfg.Lang
}

// T and Tf say a message in the language in use.
var (
	T  = locale.T
	Tf = locale.Tf
)

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
		return nil, errors.New(T(`no backend or token: run "solax set backend --url URL --token-stdin"`))
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

func first(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
