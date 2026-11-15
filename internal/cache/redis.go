// Package cache — реализация links.Cache на Redis.
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/devAL3X/shortlink/internal/links"
)

// Redis — клиент кэша редиректов.
type Redis struct {
	rdb *redis.Client
}

// New создаёт клиент Redis.
func New(addr string) *Redis {
	return &Redis{rdb: redis.NewClient(&redis.Options{Addr: addr})}
}

func (c *Redis) Close() error { return c.rdb.Close() }

// Ping используется обработчиком /healthz.
func (c *Redis) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }

// Get возвращает URL из кэша; при отсутствии ключа — links.ErrCacheMiss.
func (c *Redis) Get(ctx context.Context, code string) (string, error) {
	url, err := c.rdb.Get(ctx, "link:"+code).Result()
	if errors.Is(err, redis.Nil) {
		return "", links.ErrCacheMiss
	}
	return url, err
}

// Set кладёт URL в кэш с TTL.
func (c *Redis) Set(ctx context.Context, code, url string, ttl time.Duration) error {
	return c.rdb.Set(ctx, "link:"+code, url, ttl).Err()
}

// Del инвалидирует кэш (при изменении или удалении ссылки).
func (c *Redis) Del(ctx context.Context, code string) error {
	return c.rdb.Del(ctx, "link:"+code).Err()
}
