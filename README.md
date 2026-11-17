# Solax

Solax is an app for super-agent workflows.

You talk to one agent, the **Super Agent**. It gives jobs to coding agents
(Claude Code, Codex and pi) that run on your machines. When a job is done,
the result comes back to the Super Agent, and it replies to you.

Solax takes ideas from [Multica](https://multica.ai), [Alma](https://alma.now),
[Kero](https://kero.sh), and other great apps.

> **Status:** early work.

## How it works

Solax has three parts. One binary, `solax`, runs the server, the daemon
and the CLI.

| Part    | Command                                  | What it does                                                                                                                             |
| ------- | ---------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| Server  | `solax server run`                       | Keeps all state and routes messages. Has no LLM logic.                                                                                   |
| Daemon  | `solax daemon run`                       | Runs on each machine. Starts the agent CLIs and turns their output into one event format. Your code and credentials stay on the machine. |
| CLI     | `solax`                                  | Client commands for humans and for agents.                                                                                               |
| Desktop | [`apps/desktop`](apps/desktop/README.md) | Native macOS app, written in Go.                                                                                                         |

```text
 Desktop / CLI ──► Server ◄── Daemon (machine A) ──► Claude Code, Codex, pi
                          ◄── Daemon (machine B) ──► Claude Code, Codex, pi
```

Daemons connect out to the server, so a machine does not need an open port.

## Quick start

You need Go 1.27 or later, and at least one agent CLI
(`claude`, `codex` or `pi`) on your `PATH` or in a usual install
folder (`~/.local/bin`, Homebrew, nvm, bun).

Install the CLI:

```sh
go install ./cmd/solax
```

Run the server and the daemon on this machine:

```sh
solax setup local
```

You can run this command again at any time. It changes only what is missing.

The daemon finds the agent CLIs (claude, codex, pi) on its machine by
itself, and looks again every 30 seconds for CLIs that were installed,
updated or removed. `solax machine list` shows them.

Set up a Super Agent and one worker agent:

```sh
solax agent add router --runtime pi --dir ~ --super
solax agent add reviewer --runtime claude --dir ~/src/app \
    --desc "Reviews Go code and finds bugs" \
    --instructions-file review.md
```

The Super Agent uses `--desc` to choose an agent for a job, so write it
clearly. The instructions (`--instructions` or `--instructions-file`)
are the system prompt of the agent: its sessions get them after the
Solax role prompt. They are optional. Change them with
`solax agent edit reviewer --instructions-file review.md`; a session that
starts after the change gets the new text.

Talk to the Super Agent:

```sh
solax chat "Review the auth package in ~/src/app"
```

Or give a job to an agent yourself:

```sh
solax job send reviewer "Find bugs in internal/auth"
solax job list
solax job show <job-id>
```

To follow everything live, run `solax watch`.

### Forks of Claude Code

A CLI that speaks the stream-json of Claude Code runs on the Claude Code
runtime. Add one, or point the built-in `claude` at another binary, in
`~/.solax/config.yaml`; the base name of the binary is the runtime name.
Restart the daemon after a change.

```yaml
runtimes:
  claude_compatible:
    - mycc                  # on the PATH
    - /opt/tools/other-cc   # a path: the runtime is other-cc
    - ~/bin/claude          # the claude runtime uses this binary
```

## Language

The CLI and the desktop app speak English or Simplified Chinese.

- CLI: `solax set lang zh-Hans` (or `en`, or `auto`). `auto` follows
  `$SOLAX_LANG`, then `$LC_ALL`, `$LC_MESSAGES` and `$LANG`.
  `$SOLAX_LANG` wins over the setting. Agent sessions always get English.
- Desktop: Settings › Look › Language. It follows the system by default.

## Add another machine

On the new machine, point the CLI to your server and start the daemon:

```sh
solax daemon start --server https://your-server:8740
```

The daemon prints a pairing code. Approve it from a machine that has a
client token:

```sh
solax machine approve <code>
```

## Desktop app

The desktop app shows the chat with the Super Agent, all jobs as live
transcripts, your agents and your machines. See
[`apps/desktop/README.md`](apps/desktop/README.md).

```sh
cd apps/desktop
go run .
```

## Repository layout

| Path               | Contents                                                      |
| ------------------ | ------------------------------------------------------------- |
| `cmd/solax`        | The `solax` binary                                            |
| `internal/`        | Server, daemon, CLI, store, agent runtimes                    |
| `pkg/`             | Public packages: API client, event format (IR), wire protocol |
| `api/openapi.yaml` | REST API                                                      |
| `apps/desktop`     | macOS desktop app                                             |
| `docs/`            | Design, roadmap and progress reports                          |

## Development

Each change must pass these checks:

```sh
go build ./...
go vet ./...
go test -race ./...
```

Commit messages use [Conventional Commits](https://www.conventionalcommits.org/).

## Credits

- [Multica](https://multica.ai): server, local daemon and
  CLI; agents use the same CLI as humans.
- [Kero](https://kero.sh) by EGOIST: the desktop layout,
  palettes and themes.
- [MyGo](https://github.com/egoist/mygo) by EGOIST: the native UI
  toolkit that the desktop app is built with.
- [Alma](https://alma.now), and other great apps.
