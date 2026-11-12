// Package server builds the solax-server process: store, engine, outbox,
// hub, client stream and the gin router.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/austiecodes/solax/internal/auth"
	"github.com/austiecodes/solax/internal/core"
	"github.com/austiecodes/solax/internal/core/outbox"
	"github.com/austiecodes/solax/internal/server/api"
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

// Run starts the server and blocks until ctx ends.
func Run(ctx context.Context, cfg Config, log *slog.Logger) error {
	store, err := sqlite.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()

	verifier := auth.NewService(store, cfg.Secret)
	h := hub.New(verifier)
	st := stream.New()
	eng := core.NewEngine(core.Deps{Store: store, Dispatcher: h, Notifier: st, Tokens: verifier})
	h.SetFacts(eng)

	ob := outbox.New(store, log)
	store.OnEffects(ob.Kick)
	// TODO: register handlers for core.EffectDeliver, EffectEnsure, EffectNotifyClient.

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.NewRouter(api.Deps{Commands: eng, Enrollment: eng, Queries: eng, Auth: verifier, Daemon: h, Stream: st}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 2)
	go func() { errc <- ob.Run(ctx) }()
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
