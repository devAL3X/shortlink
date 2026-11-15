// Команда api — REST-сервис сокращателя ссылок.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/devAL3X/shortlink/internal/api"
	"github.com/devAL3X/shortlink/internal/cache"
	"github.com/devAL3X/shortlink/internal/config"
	"github.com/devAL3X/shortlink/internal/events"
	"github.com/devAL3X/shortlink/internal/links"
	"github.com/devAL3X/shortlink/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pg, err := store.New(ctx, config.Env("DATABASE_URL",
		"postgres://shortlink:shortlink@postgres-primary:5432/shortlink"))
	if err != nil {
		log.Error("connect postgres", "err", err)
		os.Exit(1)
	}
	defer pg.Close()

	rdb := cache.New(config.Env("REDIS_ADDR", "redis:6379"))
	defer rdb.Close()

	rabbit := events.New(
		config.EnvList("RABBIT_ADDRS", "rabbitmq1:5672,rabbitmq2:5672,rabbitmq3:5672"),
		config.Env("RABBIT_USER", "app"),
		config.Env("RABBIT_PASS", "app"),
		log,
	)
	defer rabbit.Close()

	svc := links.New(pg, rdb, rabbit, 5*time.Minute, log)

	srv := &http.Server{
		Addr:              config.Env("LISTEN", ":8080"),
		Handler:           api.New(svc, log),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("api server", "err", err)
			os.Exit(1)
		}
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("api shutdown", "err", err)
	}
	log.Info("api stopped")
}
