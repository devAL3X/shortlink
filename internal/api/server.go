// Package api — HTTP API сокращателя ссылок.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/devAL3X/shortlink/internal/links"
)

// pinger — компонент, чьё здоровье можно проверить (для /healthz).
type pinger interface {
	Ping(context.Context) error
}

// Server — HTTP-обработчики API.
type Server struct {
	svc *links.Service
	log *slog.Logger
}

// New собираёт маршрутизатор API.
func New(svc *links.Service, log *slog.Logger) http.Handler {
	s := &Server{svc: svc, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", s.hello)               // заглушка этапа 2
	mux.HandleFunc("GET /healthz", s.healthz)           // здоровье компонентов
	mux.HandleFunc("POST /api/links", s.create)         // INSERT
	mux.HandleFunc("GET /api/links", s.list)            // SELECT
	mux.HandleFunc("PATCH /api/links/{code}", s.update) // UPDATE
	mux.HandleFunc("DELETE /api/links/{code}", s.delete) // DELETE + инвалидация кэша
	mux.HandleFunc("GET /api/links/{code}/resolve", s.resolveInfo) // данные редиректа без 302 (для UI)
	mux.HandleFunc("GET /r/{code}", s.redirect)         // редирект с кэшем и событием клика

	return s.middleware(mux)
}

func (s *Server) hello(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("hello from shortlink api\n"))
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	type component struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	checks := map[string]component{}
	healthy := true
	check := func(name string, err error) {
		c := component{OK: err == nil}
		if err != nil {
			c.Error = err.Error()
			healthy = false
		}
		checks[name] = c
	}
	if p, ok := s.svc.Store.(pinger); ok {
		check("postgres", p.Ping(ctx))
	}
	if p, ok := s.svc.Cache.(pinger); ok {
		check("redis", p.Ping(ctx))
	}
	if p, ok := s.svc.Pub.(interface{ Status(context.Context) error }); ok {
		check("rabbitmq", p.Status(ctx))
	}

	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"status": checks})
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if !validURL(req.URL) {
		httpError(w, http.StatusBadRequest, "url must be an absolute http(s) URL")
		return
	}
	l, err := s.svc.Create(r.Context(), req.URL)
	if err != nil {
		s.log.ErrorContext(r.Context(), "create link", "err", err)
		httpError(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	ls, err := s.svc.List(r.Context())
	if err != nil {
		s.log.ErrorContext(r.Context(), "list links", "err", err)
		httpError(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, ls)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if !validURL(req.URL) {
		httpError(w, http.StatusBadRequest, "url must be an absolute http(s) URL")
		return
	}
	switch err := s.svc.Update(r.Context(), code, req.URL); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, links.ErrNotFound):
		httpError(w, http.StatusNotFound, "link not found")
	default:
		s.log.ErrorContext(r.Context(), "update link", "code", code, "err", err)
		httpError(w, http.StatusInternalServerError, "storage error")
	}
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	switch err := s.svc.Delete(r.Context(), code); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, links.ErrNotFound):
		httpError(w, http.StatusNotFound, "link not found")
	default:
		s.log.ErrorContext(r.Context(), "delete link", "code", code, "err", err)
		httpError(w, http.StatusInternalServerError, "storage error")
	}
}

// redirect — GET /r/{code}: берёт URL из кэша или БД, публикует клик
// и отвечает 302. Заголовок X-Cache показывает, откуда взят адрес.
func (s *Server) redirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	target, fromCache, err := s.svc.Resolve(r.Context(), code)
	if errors.Is(err, links.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.log.ErrorContext(r.Context(), "resolve link", "code", code, "err", err)
		httpError(w, http.StatusInternalServerError, "resolve error")
		return
	}
	if fromCache {
		w.Header().Set("X-Cache", "HIT")
	} else {
		w.Header().Set("X-Cache", "MISS")
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// resolveInfo — тот же путь разрешения кода (кэш -> БД), но JSON без 302.
// Используется UI: браузерный fetch не даёт прочитать заголовки непроследованного редиректа.
func (s *Server) resolveInfo(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	target, fromCache, err := s.svc.Resolve(r.Context(), code)
	if errors.Is(err, links.ErrNotFound) {
		httpError(w, http.StatusNotFound, "link not found")
		return
	}
	if err != nil {
		s.log.ErrorContext(r.Context(), "resolve link", "code", code, "err", err)
		httpError(w, http.StatusInternalServerError, "resolve error")
		return
	}
	cache := "MISS"
	if fromCache {
		cache = "HIT"
	}
	writeJSON(w, http.StatusOK, map[string]string{"code": code, "target": target, "cache": cache})
}

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// middleware логирует запросы и гасит паники.
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic recovered", "err", v, "path", r.URL.Path)
				httpError(rec, http.StatusInternalServerError, "internal error")
			}
			s.log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", time.Since(start).Round(time.Microsecond),
			)
		}()
		next.ServeHTTP(rec, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
