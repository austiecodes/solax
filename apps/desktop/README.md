# Solax desktop client

Native macOS app, written entirely in Go with `github.com/egoist/mygo/ui`.
No webview, no JavaScript.

Pages:

- **Super Agent**: chat with the router. Shows when it works and which
  command it runs, and the jobs that run now.
- **Jobs**: every job with its prompt, result and live activity (tool
  calls, messages, streamed text), the same view for Claude Code, Codex and
  pi. Send a follow-up or cancel.
- **Agents**: set up agents (name, machine, driver, folder, model,
  description) and choose the Super Agent.
- **Machines**: machines, the agent CLIs their daemons found, and pairing.
- **Settings**: server URL and client token. The token is kept in the
  macOS keychain (service `dev.austiecodes.solax`), the profiles in
  `~/Library/Application Support/solax/desktop/profiles.json`.

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
