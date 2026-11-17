// Package server builds the server process ("solax server run"): database,
// service functions, hub, delivery loop, client stream and the gin router.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/server/delivery"
	"github.com/austiecodes/solax/internal/server/hub"
	"github.com/austiecodes/solax/internal/server/model"
	"github.com/austiecodes/solax/internal/server/router"
	"github.com/austiecodes/solax/internal/server/service"
	"github.com/austiecodes/solax/internal/server/stream"
)

// Config configures the server.
type Config struct {
	Addr   string // for example ":8740"
	DBPath string
	Secret []byte // signs agent tokens
}

// Init creates the owner and the first client token in a new database. It
// prints the token once. "solax server init" calls it.
func Init(ctx context.Context, cfg Config, clientName string) (token string, err error) {
	if err := model.Open(ctx, cfg.DBPath); err != nil {
		return "", err
	}
	defer model.Close()
	owner, err := service.InitOwner(ctx)
	if err != nil {
		return "", err
	}
	token, _, err = auth.CreateClient(ctx, owner.ID, clientName)
	return token, err
}

// App is the assembled server: the HTTP handler and its delivery loop.
// Serve serves it; tests serve its Handler with httptest.
type App struct {
	Handler http.Handler
	hub     *hub.Hub
	loop    *delivery.Loop
}

// New assembles the server and sets the service functions. The model
// database must be open.
func New(secret []byte, log *slog.Logger) *App {
	auth.SetSecret(secret)
	h := hub.New(log)
	st := stream.New()
	loop := delivery.New(h, log)

	service.Send, service.Online, service.Disconnect = h.Send, h.Online, h.Disconnect
	service.Publish = st.Publish
	service.IssueToken = auth.IssueToken
	service.Wake = loop.Kick

	return &App{
		Handler: router.New(h, st),
		hub:     h,
		loop:    loop,
	}
}

// Close closes the daemon connections and waits for their handlers. Call
// it after the HTTP server stops and before the database closes.
func (a *App) Close() { a.hub.Close() }

// Start runs the delivery loop until ctx ends. Its error goes to errc.
func (a *App) Start(ctx context.Context, errc chan<- error) {
	go func() { errc <- a.loop.Run(ctx) }()
}

// Serve starts the server on ln and blocks until ctx ends. It closes ln.
// The caller can listen first, so a busy port fails before the database
// opens. cfg.Addr is not used.
func Serve(ctx context.Context, cfg Config, ln net.Listener, log *slog.Logger) error {
	if err := model.Open(ctx, cfg.DBPath); err != nil {
		ln.Close()
		return err
	}
	defer model.Close()
	app := New(cfg.Secret, log)
	srv := &http.Server{Handler: app.Handler, ReadHeaderTimeout: 10 * time.Second}

	errc := make(chan error, 4)
	app.Start(ctx, errc)
	go func() {
		log.Info("listening", "addr", ln.Addr().String())
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	var err error
	select {
	case <-ctx.Done():
	case err = <-errc:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = errors.Join(err, srv.Shutdown(shutdown))
	app.Close()
	return err
}

// LoadSecret returns the secret that signs agent tokens: the value of env
// if set, else the content of file, which is created with a random secret
// on first use. Agent tokens stay valid across restarts.
func LoadSecret(env, file string) ([]byte, error) {
	if env != "" {
		return []byte(env), nil
	}
	if b, err := os.ReadFile(file); err == nil && len(b) >= 32 {
		return b, nil
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	secret := []byte(hex.EncodeToString(raw[:]))
	return secret, os.WriteFile(file, secret, 0o600)
}
