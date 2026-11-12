# Solax desktop client

Native MyGo UI written entirely in Go using `github.com/egoist/mygo/ui`.
Requires Go 1.27.3. No Bun, Vite, TypeScript, or webview is required.

Run from this directory:

```sh
go1.27.3 tool mygo dev
go1.27.3 tool mygo build
go1.27.3 test ./...
```

`main.go` opens the native window; `native.go` implements Super chat,
workspaces and task messages, machines and pairing, and server settings.
Views call `services.go` directly using shared `pkg/protocol` types.
Network and keychain operations run outside the UI thread and apply results
through `Window.Update`. Tokens stay in the OS keychain.

`stream.go` delivers Go events to native listeners. Its WebSocket transport
and parts of `pkg/client` are still unimplemented; server operations report
those errors in the UI. Use Refresh to reload available server data.
