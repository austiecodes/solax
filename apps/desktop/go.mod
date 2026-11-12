module github.com/austiecodes/solax/apps/desktop

go 1.27.3

require (
	github.com/austiecodes/solax v0.0.0
	github.com/egoist/mygo v0.7.4
)

require (
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace github.com/austiecodes/solax => ../..

tool github.com/egoist/mygo/cmd/mygo
