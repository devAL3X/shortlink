// Команда frontend — единственная точка входа извне: отдаёт статический UI
// и проксирует запросы к API во внутреннем сегменте сети.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/devAL3X/shortlink/internal/config"
	"github.com/devAL3X/shortlink/internal/web"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	apiURL, err := url.Parse(config.Env("API_ADDR", "http://api:8080"))
	if err != nil {
		log.Error("bad API_ADDR", "err", err)
		os.Exit(1)
	}
	proxy := httputil.NewSingleHostReverseProxy(apiURL)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Warn("proxy error", "path", r.URL.Path, "err", err)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("api unavailable\n"))
	}

	// UI — один встроенный файл: запрещаем браузеру кэшировать,
	// иначе после деплоя обновления не видны без жёсткого обновления
	fileServer := http.FileServerFS(web.Static())
	ui := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	})

	mux := http.NewServeMux()
	mux.Handle("/api/", proxy)
	mux.Handle("/r/", proxy)
	mux.Handle("GET /healthz", proxy)
	mux.Handle("GET /hello", proxy)
	mux.Handle("/", ui)

	srv := &http.Server{
		Addr:              config.Env("LISTEN", ":8080"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("frontend listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("frontend server", "err", err)
			os.Exit(1)
		}
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("frontend shutdown", "err", err)
	}
	log.Info("frontend stopped")
}
