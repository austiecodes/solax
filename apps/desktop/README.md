# Solax desktop client

Native macOS app, written entirely in Go with `github.com/egoist/mygo/ui`.
No webview, no JavaScript.

Pages:

- **Solax**: chat with Solax, the agent you talk to (the "Super Agent"
  role on the server). It gives tasks to your other agents. Between its replies, what it
  did (reasoning and tool calls) folds into a group: "4 tools used". A
  group is open while the agent works and closes when it is done; a
  click opens or closes it, and a click on a tool call shows its output.
  Return sends, Shift+Return breaks the line.
- **Tasks**: the list of all tasks, or one task as the same transcript: its
  prompt, the steps of the agent, its replies and live text. It looks the
  same for Claude Code, Codex and pi. Send a follow-up or cancel.
- **Agents**: set up agents (name, machine, runtime, folder, model,
  description, instructions), edit the instructions of an agent, and
  choose the agent that is Solax.
- **Machines**: machines, the agent CLIs their daemons found, and pairing.
- **Settings**: server URL and client token; theme, appearance and
  language (English or Simplified Chinese, the system's by default). The token is kept in the
  macOS keychain (service `dev.austiecodes.solax`), the profiles in
  `~/Library/Application Support/solax/desktop/profiles.json`.

Shortcuts: ⌘K finds a task, an agent or an action; ⌘1–⌘4 open the pages, ⌘, opens Settings, ⌘B hides the
sidebar, ⇧⌘L toggles dark mode, ⌘R reloads. The window remembers its page,
the open task, the sidebar and the unread tasks in `app.json`, next to
`profiles.json`.

A task that ends while you do not look at it is unread (bold in the
sidebar). While the window is in the back, a finished or failed task
also shows a notification; a click opens it. The Dock badge counts
unread failed tasks.

Code map:

- `internal/actions`: the action list. The menu bar, shortcuts and
  tooltips read it. Add an action here, then handle it in `run`
  (`actions.go`).
- `theme.go`: the palettes and themes: graphite (the default, flat and
  neutral as the Codex app) and Kero's. Views use `paletteFor(c)`, not raw
  theme colors. Settings › Look chooses the theme.
- `view.go`: the window shell (sidebar, card, empty state). A new page is
  one `navPages` entry (`state.go`), one action and one case in `pageView`.
- `layout.go`: the page kit, after the Codex app's settings: `showPage` (a header
  over a column of sections), `section`, `row` (a label and help at the
  left, a control at the right), `listItem`, `chip` and `dot`. Pages use
  it, so the spacing is the same everywhere.
- `sidebar.go`: the pages, the recent tasks with their state, and the
  footer counts (queued or running, failed, done and unread).
- `taskgroups.go`: how the sidebar groups the tasks: by project (the work
  folder of the task's agent), by agent, or not at all. The menu next to
  "Recent tasks" and Settings › Sidebar choose it.
- `controls.go`: the app's buttons, text field, text area, select,
  segmented control and task status icon. Pages use these, not the stock
  `ui.Button`/`ui.TextInput`.
- `transcript.go`: turns IR events (and, for the chat, the messages) into
  blocks: user messages, replies, groups of steps, notes. No UI, so the
  tests cover the rules. `transcript_view.go` draws the blocks, the
  folds and the composer.
- `chat.go`, `tasks.go`, `workflows.go`, `agents.go`, `settings.go`: the pages.
- `timeline.go`: turns the steps of a workflow run into phases, loops
  with their rounds, and stages of steps that ran at the same time. No
  UI, so the tests cover the rules. `run_view.go` shows it in the view
  the user picks: `graph_view.go` (nodes and edges, a frame for each
  loop) or `rail_view.go` (phase stations and a line down the steps).
- `palette.go`: the ⌘K palette. Each kind of item (tasks, actions,
  agents) comes from one source function in `paletteItems`.
- `unread.go`: unread tasks, notifications and the Dock badge, all in one
  place (`taskEnded`, `markRead`, `setTasks`).
- `confirm.go`: `d.ask(...)` asks before an action that cannot be undone.
- `locales/`: the messages, `en.json` (with descriptions) and
  `zh-Hans.json`. A new message goes in both; `mygo generate` writes its
  function in `locales/catalog_gen.go`, which is committed. `lang.go`
  holds the titles of actions, the words of states and the language
  choice.
- `icons/`: SVG icons, embedded (licenses in `icons/README.md`).

Run from this directory:

```sh
go run .                                        # run it
go run github.com/egoist/mygo/cmd/mygo dev      # run with hot reload
go run github.com/egoist/mygo/cmd/mygo build    # build/darwin-arm64/Solax.app
go run github.com/egoist/mygo/cmd/mygo generate # locales/catalog_gen.go
go test ./...
SOLAX_SHOTS=../../tmp/shots go test -run TestShots .  # draw each page to a PNG in the repo tmp/
```

`go.mod` has no `tool` directive on purpose: some Go version switchers
cannot parse it. Use `go run github.com/egoist/mygo/cmd/mygo` instead of
`go tool mygo`.

## Credits

The look follows the Codex app: flat panes, neutral grays, the
composer, the tasks grouped by project, and the settings pages.

The code started from [Kero](https://github.com/egoist/kero) by EGOIST,
the author of MyGo (MIT). Solax takes from Kero:

- the color palettes and the themes (paper, mist, meadow, dusk, tide,
  apricot, aurora, dune, petal, plain), with their elevation (`theme.go`);
- the task status icons;
- the action list, the ⌘K palette and the confirm dialog;
- the rules for unread tasks and notifications;
- the icon sets (`icons/README.md`).

Thanks to Kero for a clear example of a native MyGo app.
