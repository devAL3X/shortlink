// Package links содержит доменную логику сокращателя ссылок:
// сервис работает с хранилищем (PostgreSQL), кэшем (Redis)
// и брокером сообщений (RabbitMQ) через небольшие интерфейсы.
package links

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"log/slog"
	"time"
)

// ErrNotFound возвращается, когда ссылка с таким кодом отсутствует в БД.
var ErrNotFound = errors.New("link not found")

// ErrCacheMiss возвращается кэшем при промахе.
var ErrCacheMiss = errors.New("cache miss")

// Link — короткая ссылка и её статистика.
type Link struct {
	Code      string    `json:"code"`
	URL       string    `json:"url"`
	Clicks    int64     `json:"clicks"`
	CreatedAt time.Time `json:"created_at"`
}

// alphabet без похожих символов (0/O, 1/l/I), чтобы коды читались с экрана.
const alphabet = "23456789abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ"

// NewCode генерирует криптографически случайный код длины n.
func NewCode(n int) string {
	b := make([]byte, n)
	for i := range b {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			panic(err) // отказ crypto/rand делает дальнейшую работу бессмысленной
		}
		b[i] = alphabet[v.Int64()]
	}
	return string(b)
}

// Store — хранение ссылок в PostgreSQL.
type Store interface {
	Create(ctx context.Context, url string) (Link, error) // INSERT
	Get(ctx context.Context, code string) (Link, error)   // SELECT одной строки
	List(ctx context.Context) ([]Link, error)             // SELECT списка
	UpdateURL(ctx context.Context, code, url string) error // UPDATE
	Delete(ctx context.Context, code string) error        // DELETE
	RecordClick(ctx context.Context, code string) error   // UPDATE счётчика + INSERT события
}

// Cache — кэш редиректов в Redis.
type Cache interface {
	Get(ctx context.Context, code string) (string, error)
	Set(ctx context.Context, code, url string, ttl time.Duration) error
	Del(ctx context.Context, code string) error
}

// Publisher — публикация событий клика в RabbitMQ.
type Publisher interface {
	PublishClicked(ctx context.Context, code string) error
}

// Service связывает хранилище, кэш и брокер.
type Service struct {
	Store    Store
	Cache    Cache
	Pub      Publisher
	CacheTTL time.Duration
	log      *slog.Logger
}

// New создаёт сервис; log может быть nil — тогда используется slog.Default().
func New(store Store, cache Cache, pub Publisher, ttl time.Duration, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{Store: store, Cache: cache, Pub: pub, CacheTTL: ttl, log: log}
}

// Create сокращает url: INSERT в БД.
func (s *Service) Create(ctx context.Context, url string) (Link, error) {
	return s.Store.Create(ctx, url)
}

// List возвращает все ссылки со статистикой.
func (s *Service) List(ctx context.Context) ([]Link, error) {
	return s.Store.List(ctx)
}

// Update меняет целевой URL и сбрасывает кэш.
func (s *Service) Update(ctx context.Context, code, url string) error {
	if err := s.Store.UpdateURL(ctx, code, url); err != nil {
		return err
	}
	return s.Cache.Del(ctx, code)
}

// Delete удаляет ссылку из БД и кэша.
func (s *Service) Delete(ctx context.Context, code string) error {
	if err := s.Store.Delete(ctx, code); err != nil {
		return err
	}
	return s.Cache.Del(ctx, code)
}

// Resolve возвращает целевой URL для редиректа.
// Сначала проверяется Redis, при промахе — PostgreSQL с прогревом кэша.
// Второе значение сообщает, был ли ответ взят из кэша (для заголовка X-Cache).
// Публикация события клика — fire-and-forget: редирект не должен падать из-за брокера.
func (s *Service) Resolve(ctx context.Context, code string) (target string, fromCache bool, err error) {
	url, err := s.Cache.Get(ctx, code)
	switch {
	case err == nil:
		s.publishClicked(ctx, code)
		return url, true, nil
	case errors.Is(err, ErrCacheMiss):
		// идём в БД
	default:
		s.log.WarnContext(ctx, "cache get failed", "code", code, "err", err)
	}

	l, err := s.Store.Get(ctx, code)
	if err != nil {
		return "", false, err
	}
	if err := s.Cache.Set(ctx, code, l.URL, s.CacheTTL); err != nil {
		s.log.WarnContext(ctx, "cache set failed", "code", code, "err", err)
	}
	s.publishClicked(ctx, code)
	return l.URL, false, nil
}

func (s *Service) publishClicked(ctx context.Context, code string) {
	if err := s.Pub.PublishClicked(ctx, code); err != nil {
		s.log.WarnContext(ctx, "publish link.clicked failed", "code", code, "err", err)
	}
}
