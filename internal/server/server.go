// Package server builds the solax-server process: store, engine, outbox,
// hub, client stream and the gin router.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/core/outbox"
	"github.com/austiecodes/solax/internal/server/api"
	"github.com/austiecodes/solax/internal/server/delivery"
	"github.com/austiecodes/solax/internal/server/hub"
	"github.com/austiecodes/solax/internal/server/stream"
	"github.com/austiecodes/solax/internal/store/sqlite"
)

// Config configures the server.
type Config struct {
	Addr   string // for example ":8740"
	DBPath string
	Secret []byte // signs agent tokens
}

// Init creates the owner and the first client token in a new database. It
// prints the token once. "solax-server init" calls it.
func Init(ctx context.Context, cfg Config, clientName string) (token string, err error) {
	store, err := sqlite.Open(ctx, cfg.DBPath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	owner, err := core.NewEngine(core.Deps{Store: store}).InitOwner(ctx)
	if err != nil {
		return "", err
	}
	token, _, err = auth.NewService(store, cfg.Secret).CreateClient(ctx, owner.ID, clientName)
	return token, err
}

// Admin opens the database for "solax-server token ..." commands. They
// run on the server host and need no token.
func Admin(ctx context.Context, cfg Config) (*auth.Service, func() error, error) {
	store, err := sqlite.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, nil, err
	}
	return auth.NewService(store, cfg.Secret), store.Close, nil
}

// App is the assembled server: the HTTP handler and its background loops.
// Run serves it; tests serve its Handler with httptest.
type App struct {
	Handler http.Handler
	Engine  *core.Engine
	Auth    *auth.Service
	loops   []func(context.Context) error
}

// New assembles the server on an open store.
func New(store *sqlite.Store, secret []byte, log *slog.Logger) *App {
	verifier := auth.NewService(store, secret)
	h := hub.New(verifier, log)
	st := stream.New()
	eng := core.NewEngine(core.Deps{Store: store, Dispatcher: h, Notifier: st, Tokens: verifier})
	h.SetFacts(eng)
	loop := delivery.New(eng, h, log)
	eng.OnWork(loop.Kick)
	ob := outbox.New(store, log)
	store.OnEffects(ob.Kick)
	return &App{
		Handler: api.NewRouter(api.Deps{Commands: eng, Enrollment: eng, Queries: eng, Auth: verifier, Daemon: h, Stream: st}),
		Engine:  eng,
		Auth:    verifier,
		loops:   []func(context.Context) error{ob.Run, loop.Run},
	}
}

// Start runs the background loops until ctx ends. Errors go to errc.
func (a *App) Start(ctx context.Context, errc chan<- error) {
	for _, run := range a.loops {
		go func() { errc <- run(ctx) }()
	}
}

// Run starts the server and blocks until ctx ends.
func Run(ctx context.Context, cfg Config, log *slog.Logger) error {
	store, err := sqlite.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()
	app := New(store, cfg.Secret, log)
	srv := &http.Server{Addr: cfg.Addr, Handler: app.Handler, ReadHeaderTimeout: 10 * time.Second}

	errc := make(chan error, 4)
	app.Start(ctx, errc)
	go func() {
		log.Info("listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	select {
	case <-ctx.Done():
	case err = <-errc:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return errors.Join(err, srv.Shutdown(shutdown))
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
