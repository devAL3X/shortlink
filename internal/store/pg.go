// Package store — реализация links.Store на PostgreSQL (pgx).
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devAL3X/shortlink/internal/links"
)

// PG — пул соединений к PostgreSQL.
type PG struct {
	pool *pgxpool.Pool
}

// New создаёт пул соединений и проверяет доступность БД.
func New(ctx context.Context, dsn string) (*PG, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pg pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping pg: %w", err)
	}
	return &PG{pool: pool}, nil
}

func (p *PG) Close() { p.pool.Close() }

// Ping используется обработчиком /healthz.
func (p *PG) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

func isUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

// Create — INSERT новой ссылки со случайным кодом.
func (p *PG) Create(ctx context.Context, url string) (links.Link, error) {
	var l links.Link
	// коллизии кодов практически невозможны, но одна повторная попытка дешёвая
	for range 3 {
		code := links.NewCode(6)
		err := p.pool.QueryRow(ctx,
			`INSERT INTO links (code, url)
			 VALUES ($1, $2)
			 RETURNING code, url, clicks, created_at`,
			code, url,
		).Scan(&l.Code, &l.URL, &l.Clicks, &l.CreatedAt)
		if err == nil {
			return l, nil
		}
		if isUniqueViolation(err) {
			continue
		}
		return links.Link{}, fmt.Errorf("insert link: %w", err)
	}
	return links.Link{}, errors.New("could not generate unique code")
}

// Get — SELECT одной ссылки по коду.
func (p *PG) Get(ctx context.Context, code string) (links.Link, error) {
	var l links.Link
	err := p.pool.QueryRow(ctx,
		`SELECT code, url, clicks, created_at FROM links WHERE code = $1`, code,
	).Scan(&l.Code, &l.URL, &l.Clicks, &l.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return links.Link{}, links.ErrNotFound
	}
	if err != nil {
		return links.Link{}, fmt.Errorf("select link: %w", err)
	}
	return l, nil
}

// List — SELECT всех ссылок (новые сверху).
func (p *PG) List(ctx context.Context) ([]links.Link, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT code, url, clicks, created_at FROM links ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("select links: %w", err)
	}
	defer rows.Close()

	out := []links.Link{}
	for rows.Next() {
		var l links.Link
		if err := rows.Scan(&l.Code, &l.URL, &l.Clicks, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// UpdateURL — UPDATE целевого адреса.
func (p *PG) UpdateURL(ctx context.Context, code, url string) error {
	tag, err := p.pool.Exec(ctx,
		`UPDATE links SET url = $2 WHERE code = $1`, code, url)
	if err != nil {
		return fmt.Errorf("update link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return links.ErrNotFound
	}
	return nil
}

// Delete — DELETE ссылки.
func (p *PG) Delete(ctx context.Context, code string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM links WHERE code = $1`, code)
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return links.ErrNotFound
	}
	return nil
}

// RecordClick инкрементирует счётчик кликов (UPDATE)
// и добавляет строку в журнал событий (INSERT).
// Вызывается воркером после получения сообщения из RabbitMQ.
func (p *PG) RecordClick(ctx context.Context, code string) error {
	tag, err := p.pool.Exec(ctx,
		`UPDATE links SET clicks = clicks + 1 WHERE code = $1`, code)
	if err != nil {
		return fmt.Errorf("update clicks: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil // ссылку удалили до обработки события — ничего не делаем
	}
	_, err = p.pool.Exec(ctx,
		`INSERT INTO link_events (code, event) VALUES ($1, 'clicked')`, code)
	if err != nil {
		return fmt.Errorf("insert link_event: %w", err)
	}
	return nil
}
