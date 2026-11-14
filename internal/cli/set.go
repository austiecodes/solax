package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/internal/localconfig"
)

func setCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "set", Short: "Change the local configuration"}
	cmd.AddCommand(setBackendCmd(), setMachineCmd())
	return cmd
}

func setBackendCmd() *cobra.Command {
	var (
		rawURL     string
		tokenStdin bool
	)
	cmd := &cobra.Command{
		Use:   "backend",
		Short: "Set the server that this machine uses",
		Example: `  solax set backend --url https://solax.example.com
  echo "$TOKEN" | solax set backend --url https://solax.example.com --token-stdin`,
		RunE: func(cmd *cobra.Command, args []string) error {
			u, err := url.Parse(rawURL)
			if err != nil || u.Scheme == "" || u.Host == "" {
				return fmt.Errorf("--url: want an absolute URL, for example https://solax.example.com")
			}
			p, err := paths()
			if err != nil {
				return err
			}
			token := ""
			if tokenStdin {
				if token, err = readToken(cmd.InOrStdin()); err != nil {
					return err
				}
			}
			_, err = saveBackend(p, u.String(), token, false)
			return err
		},
	}
	cmd.Flags().StringVar(&rawURL, "url", "", "server URL")
	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "read the client token from stdin")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}

func setMachineCmd() *cobra.Command {
	var (
		name        string
		maxSessions int
	)
	cmd := &cobra.Command{
		Use:   "machine",
		Short: "Set the daemon settings of this machine",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths()
			if err != nil {
				return err
			}
			cfg, err := p.Load()
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("name") {
				cfg.Machine.Name = name
			}
			if cmd.Flags().Changed("max-sessions") {
				if maxSessions <= 0 {
					return errors.New("--max-sessions: want a positive number")
				}
				cfg.Machine.MaxSessions = maxSessions
			}
			return p.Save(cfg)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "machine name")
	cmd.Flags().IntVar(&maxSessions, "max-sessions", 0, "maximum concurrent agent sessions")
	return cmd
}

// saveBackend sets the server of this machine and, if token is not empty,
// the client token. The machine identity belongs to one server, so a new
// URL, or resetMachine, removes it; the next "daemon start" sets the
// machine up again. It reports if the machine identity was removed.
func saveBackend(p localconfig.Paths, rawURL, token string, resetMachine bool) (machineReset bool, err error) {
	cfg, err := p.Load()
	if err != nil {
		return false, err
	}
	creds, err := p.LoadCredentials()
	if err != nil {
		return false, err
	}
	u := strings.TrimRight(rawURL, "/")
	if resetMachine || (cfg.Backend.URL != "" && cfg.Backend.URL != u) {
		machineReset = creds.MachineID != "" || creds.MachineToken != ""
		creds.MachineID, creds.MachineToken = "", ""
	}
	cfg.Backend.URL = u
	if token != "" {
		creds.ClientToken = token
	}
	if err := p.Save(cfg); err != nil {
		return false, err
	}
	return machineReset, p.SaveCredentials(creds)
}

func configCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show the effective local configuration (tokens masked)",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths()
			if err != nil {
				return err
			}
			cfg, err := p.LoadEffective()
			if err != nil {
				return err
			}
			creds, err := p.LoadCredentials()
			if err != nil {
				return err
			}
			view := struct {
				Home        string                  `json:"home"`
				Config      localconfig.Config      `json:"config"`
				Credentials localconfig.Credentials `json:"credentials"`
			}{p.Dir, cfg, localconfig.Credentials{
				ClientToken:  mask(creds.ClientToken),
				MachineID:    creds.MachineID,
				MachineToken: mask(creds.MachineToken),
			}}
			return g.print(cmd.OutOrStdout(), view, func(w io.Writer) {
				fmt.Fprintf(w, "home:          %s\n", p.Dir)
				fmt.Fprintf(w, "backend.url:   %s\n", cfg.Backend.URL)
				fmt.Fprintf(w, "client token:  %s\n", view.Credentials.ClientToken)
				fmt.Fprintf(w, "machine:       %s (max %d sessions)\n", cfg.Machine.Name, cfg.Machine.MaxSessions)
				fmt.Fprintf(w, "machine id:    %s\n", creds.MachineID)
				fmt.Fprintf(w, "machine token: %s\n", view.Credentials.MachineToken)
			})
		},
	}
}

func readToken(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	t := strings.TrimSpace(line)
	if t == "" {
		return "", errors.New("--token-stdin: empty token")
	}
	return t, nil
}

func mask(s string) string {
	switch {
	case s == "":
		return ""
	case len(s) <= 8:
		return "****"
	default:
		return s[:4] + "…" + s[len(s)-4:]
	}
}
