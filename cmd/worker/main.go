// Команда worker — потребитель событий кликов из RabbitMQ:
// инкрементирует счётчики и пишет журнал событий в PostgreSQL.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/devAL3X/shortlink/internal/config"
	"github.com/devAL3X/shortlink/internal/events"
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

	rabbit := events.New(
		config.EnvList("RABBIT_ADDRS", "rabbitmq1:5672,rabbitmq2:5672,rabbitmq3:5672"),
		config.Env("RABBIT_USER", "app"),
		config.Env("RABBIT_PASS", "app"),
		log,
	)
	defer rabbit.Close()

	handle := func(ctx context.Context, code string) {
		if err := pg.RecordClick(ctx, code); err != nil {
			log.Warn("record click", "code", code, "err", err)
			return
		}
		log.Info("click recorded", "code", code)
	}

	log.Info("worker started")
	if err := rabbit.ConsumeClicked(ctx, handle); err != nil {
		log.Error("worker stopped", "err", err)
		os.Exit(1)
	}
	log.Info("worker stopped")
}
