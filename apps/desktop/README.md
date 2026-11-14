# Solax desktop client

Native macOS app, written entirely in Go with `github.com/egoist/mygo/ui`.
No webview, no JavaScript.

Pages:

- **Super Agent**: chat with the router. Between its replies, what it
  did (reasoning and tool calls) folds into a group: "4 tools used". A
  group is open while the agent works and closes when it is done; a
  click opens or closes it, and a click on a tool call shows its output.
  Return sends, Shift+Return breaks the line.
- **Jobs**: the list of all jobs, or one job as the same transcript: its
  prompt, the steps of the agent, its replies and live text. It looks the
  same for Claude Code, Codex and pi. Send a follow-up or cancel.
- **Agents**: set up agents (name, machine, driver, folder, model,
  description) and choose the Super Agent.
- **Machines**: machines, the agent CLIs their daemons found, and pairing.
- **Settings**: server URL and client token. The token is kept in the
  macOS keychain (service `dev.austiecodes.solax`), the profiles in
  `~/Library/Application Support/solax/desktop/profiles.json`.

Shortcuts: ⌘K finds a job, an agent or an action; ⌘1–⌘4 open the pages, ⌘, opens Settings, ⌘B hides the
sidebar, ⇧⌘L toggles dark mode, ⌘R reloads. The window remembers its page,
the open job, the sidebar and the unread jobs in `app.json`, next to
`profiles.json`.

A job that ends while you do not look at it is unread (bold in the
sidebar). While the window is in the back, a finished or failed job
also shows a notification; a click opens it. The Dock badge counts
unread failed jobs.

Code map:

- `internal/actions`: the action list. The menu bar, shortcuts and
  tooltips read it. Add an action here, then handle it in `run`
  (`actions.go`).
- `theme.go`: Kero's palettes and themes. Views use `paletteFor(c)`,
  not raw theme colors. Settings › Look chooses the theme.
- `view.go`: the window shell (card, header, empty state). A new page is
  one `navPages` entry (`state.go`), one action and one case in `pageView`.
- `sidebar.go`: the pages, the recent jobs with their state, and the
  footer counts (queued or running, failed, done and unread).
- `controls.go`: the app's buttons, text field and job status icon. Pages
  use these, not the stock `ui.Button`/`ui.TextInput`.
- `transcript.go`: turns IR events (and, for the chat, the messages) into
  blocks: user messages, replies, groups of steps, notes. No UI, so the
  tests cover the rules. `transcript_view.go` draws the blocks, the
  folds and the composer.
- `chat.go`, `jobs.go`, `agents.go`, `settings.go`: the pages.
- `palette.go`: the ⌘K palette. Each kind of item (jobs, actions,
  agents) comes from one source function in `paletteItems`.
- `unread.go`: unread jobs, notifications and the Dock badge, all in one
  place (`jobEnded`, `markRead`, `setJobs`).
- `confirm.go`: `d.ask(...)` asks before an action that cannot be undone.
- `icons/`: SVG icons, embedded (licenses in `icons/README.md`).

Run from this directory:

```sh
go run .                                        # run it
go run github.com/egoist/mygo/cmd/mygo dev      # run with hot reload
go run github.com/egoist/mygo/cmd/mygo build    # build/darwin-arm64/Solax.app
go test ./...
```

`go.mod` has no `tool` directive on purpose: some Go version switchers
cannot parse it. Use `go run github.com/egoist/mygo/cmd/mygo` instead of
`go tool mygo`.

## Credits

The design follows [Kero](https://github.com/egoist/kero) by EGOIST, the
author of MyGo (MIT). Solax takes from Kero:

- the window layout: a sidebar on the backdrop, the page on a card, the
  traffic lights over the sidebar;
- the color palettes and the themes (paper, mist, meadow, dusk, tide,
  apricot, aurora, dune, petal, plain), with their elevation (`theme.go`);
- the controls, the job status icons and the sidebar rows;
- the action list, the ⌘K palette and the confirm dialog;
- the rules for unread jobs and notifications;
- the icon sets (`icons/README.md`).

Thanks to Kero for a clear example of a native MyGo app.
