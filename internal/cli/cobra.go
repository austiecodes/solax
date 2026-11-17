package cli

import (
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/austiecodes/solax/internal/locale"
)

// localizeCobra says the words of cobra in the language in use: the
// headings of the help, the help command and flags, and the completion
// command. Cobra's own messages are English; errors go through
// cobraError.
func localizeCobra(root *cobra.Command) {
	if locale.Lang() == locale.English {
		return
	}
	cobra.AddTemplateFunc("tflags", localizeFlagUsages)
	root.SetUsageTemplate(usageTemplate())
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		switch c.Name() {
		case "help":
			c.Short = T("Help about any command")
			c.Long = Tf("Help provides help for any command in the application.\nSimply type %s help [path to command] for full details.", root.Name())
		case "completion":
			c.Short = T("Generate the autocompletion script for the specified shell")
			for _, sh := range c.Commands() {
				sh.Short = Tf("Generate the autocompletion script for %s", sh.Name())
				if f := sh.Flags().Lookup("no-descriptions"); f != nil {
					f.Usage = T("disable completion descriptions")
				}
			}
		}
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.InitDefaultHelpFlag()
		if f := c.Flags().Lookup("help"); f != nil {
			f.Usage = Tf("help for %s", c.Name())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

// usageTemplate is cobra's usage template with its headings translated.
func usageTemplate() string {
	return `{{T "Usage:"}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

{{T "Aliases:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{T "Examples:"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{T "Available Commands:"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{T "Additional Commands:"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{T "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces | tflags}}{{end}}{{if .HasAvailableInheritedFlags}}

{{T "Global Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces | tflags}}{{end}}{{if .HasHelpSubCommands}}

{{T "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

{{Tf "Use \"%s [command] --help\" for more information about a command." .CommandPath}}{{end}}
`
}

func init() {
	cobra.AddTemplateFunc("T", locale.T)
	cobra.AddTemplateFunc("Tf", func(format string, args ...any) string { return locale.Tf(format, args...) })
}

// pflagDefault is what pflag writes after a flag usage: its default.
var pflagDefault = regexp.MustCompile(`(?m) \(default (.*)\)$`)

// localizeFlagUsages translates what pflag adds to a flag usage: the
// default value.
func localizeFlagUsages(s string) string {
	return pflagDefault.ReplaceAllStringFunc(s, func(m string) string {
		v := pflagDefault.FindStringSubmatch(m)[1]
		return Tf(" (default %s)", v)
	})
}

// cobraErrors are the messages of cobra and pflag, as regular
// expressions, with their English format for the translation.
var cobraErrors = []struct {
	re     *regexp.Regexp
	format string
}{
	{regexp.MustCompile(`^unknown command "(.*)" for "(.*)"$`), `unknown command "%s" for "%s"`},
	{regexp.MustCompile(`^required flag\(s\) (.*) not set$`), `required flag(s) %s not set`},
	{regexp.MustCompile(`^accepts (\d+) arg\(s\), received (\d+)$`), `accepts %s arg(s), received %s`},
	{regexp.MustCompile(`^accepts at most (\d+) arg\(s\), received (\d+)$`), `accepts at most %s arg(s), received %s`},
	{regexp.MustCompile(`^requires at least (\d+) arg\(s\), only received (\d+)$`), `requires at least %s arg(s), only received %s`},
	{regexp.MustCompile(`^unknown flag: (.*)$`), `unknown flag: %s`},
	{regexp.MustCompile(`^unknown shorthand flag: (.*)$`), `unknown shorthand flag: %s`},
	{regexp.MustCompile(`^flag needs an argument: (.*)$`), `flag needs an argument: %s`},
	{regexp.MustCompile(`^invalid argument (.*)$`), `invalid argument %s`},
}

// cobraError says an error of cobra or pflag in the language in use. Any
// other error is said as it is: its messages are translated where they
// are made.
func cobraError(err error) string {
	msg := err.Error()
	if locale.Lang() == locale.English {
		return msg
	}
	first, rest, _ := strings.Cut(msg, "\n")
	for _, e := range cobraErrors {
		m := e.re.FindStringSubmatch(first)
		if m == nil {
			continue
		}
		args := make([]any, len(m)-1)
		for i, v := range m[1:] {
			args[i] = v
		}
		out := Tf(e.format, args...)
		if rest != "" {
			out += "\n" + rest
		}
		return out
	}
	return msg
}
