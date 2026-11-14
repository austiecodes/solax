# Solax

Solax is an app for super-agent workflows.

You talk to one agent, the **Super Agent**. It gives jobs to coding agents
(Claude Code, Codex and pi) that run on your machines. When a job is done,
the result comes back to the Super Agent, and it replies to you.

Solax takes ideas from Multica, Alma, Kero, and other great apps.

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
(`claude`, `codex` or `pi`) on your `PATH`.

Install the CLI:

```sh
go install ./cmd/solax
```

Run the server and the daemon on this machine:

```sh
solax setup local
```

You can run this command again at any time. It changes only what is missing.

Set up a Super Agent and one worker agent:

```sh
solax agent add router --driver pi --dir ~ --super
solax agent add reviewer --driver claude --dir ~/src/app \
    --desc "Reviews Go code and finds bugs"
```

The Super Agent uses `--desc` to choose an agent for a job, so write it
clearly.

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
| `internal/`        | Server, daemon, CLI, store, agent drivers                     |
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

- Multica: server, local daemon and
  CLI; agents use the same CLI as humans.
- [Kero](https://github.com/egoist/kero) by EGOIST: the desktop layout,
  palettes and themes.
- Alma, and other great apps.
