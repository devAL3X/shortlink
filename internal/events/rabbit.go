// Package events — работа с RabbitMQ: публикация событий клика
// и их потребление. Соединение устойчиво к недоступности брокера:
// клиент держит список узлов кластера и переключается между ними.
package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/url"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// QueueClicked — очередь событий кликов; quorum-очередь переживает
// отказ отдельного узла кластера RabbitMQ.
const QueueClicked = "link.clicked"

// ClickedHandler обрабатывает событие клика (инкремент в БД).
type ClickedHandler func(ctx context.Context, code string)

// Rabbit — подключение к кластеру RabbitMQ с автопереподключением.
type Rabbit struct {
	addrs []string
	user  string
	pass  string
	log   *slog.Logger

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

// New создаёт клиент; addrs — список узлов кластера (host:port).
func New(addrs []string, user, pass string, log *slog.Logger) *Rabbit {
	return &Rabbit{addrs: addrs, user: user, pass: pass, log: log}
}

// Close закрывает текущее соединение.
func (r *Rabbit) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeLocked()
}

func (r *Rabbit) closeLocked() {
	if r.ch != nil {
		r.ch.Close()
		r.ch = nil
	}
	if r.conn != nil {
		r.conn.Close()
		r.conn = nil
	}
}

// соединение с узлами перебором, начиная со случайного для балансировки
func (r *Rabbit) dialLocked() error {
	r.closeLocked()
	order := rand.Perm(len(r.addrs))
	var lastErr error
	for _, i := range order {
		uri := url.URL{
			Scheme: "amqp",
			User:   url.UserPassword(r.user, r.pass),
			Host:   r.addrs[i],
		}
		conn, err := amqp.Dial(uri.String())
		if err != nil {
			lastErr = err
			r.log.Warn("rabbit node unavailable", "addr", r.addrs[i], "err", err)
			continue
		}
		ch, err := conn.Channel()
		if err != nil {
			conn.Close()
			lastErr = err
			continue
		}
		_, err = ch.QueueDeclare(QueueClicked, true, false, false, false,
			amqp.Table{"x-queue-type": "quorum"})
		if err != nil {
			ch.Close()
			conn.Close()
			lastErr = err
			continue
		}
		r.conn, r.ch = conn, ch
		r.log.Info("rabbit connected", "addr", r.addrs[i])
		return nil
	}
	return fmt.Errorf("all rabbit nodes unavailable: %w", lastErr)
}

func (r *Rabbit) ensureLocked() error {
	if r.ch != nil && !r.ch.IsClosed() {
		return nil
	}
	return r.dialLocked()
}

type clickedEvent struct {
	Code string    `json:"code"`
	At   time.Time `json:"at"`
}

// PublishClicked публикует событие клика в очередь link.clicked.
func (r *Rabbit) PublishClicked(ctx context.Context, code string) error {
	body, err := json.Marshal(clickedEvent{Code: code, At: time.Now().UTC()})
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	publish := func() error {
		return r.ch.PublishWithContext(ctx, "", QueueClicked, false, false, amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
			Body:         body,
		})
	}
	if err := r.ensureLocked(); err != nil {
		return err
	}
	if err := publish(); err == nil {
		return nil
	}
	// одна повторная попытка после переподключения (узел мог умереть)
	if err := r.dialLocked(); err != nil {
		return err
	}
	return publish()
}

// Status проверяет доступность брокера; используется в /healthz.
func (r *Rabbit) Status(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ensureLocked()
}

// ConsumeClicked читает события кликов и вызывает handle.
// При обрыве соединения переподключается, пока не отменён контекст.
func (r *Rabbit) ConsumeClicked(ctx context.Context, handle ClickedHandler) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := r.consumeOnce(ctx, handle); ctx.Err() != nil {
			return ctx.Err()
		} else if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (r *Rabbit) consumeOnce(ctx context.Context, handle ClickedHandler) error {
	r.mu.Lock()
	if err := r.ensureLocked(); err != nil {
		r.mu.Unlock()
		return err
	}
	ch := r.ch
	conn := r.conn
	r.mu.Unlock()

	deliveries, err := ch.Consume(QueueClicked, "shortlink-worker", false, false, false, false, nil)
	if err != nil {
		return err
	}
	r.log.Info("consuming started", "queue", QueueClicked)

	notifyClose := conn.NotifyClose(make(chan *amqp.Error, 1))
	for {
		select {
		case <-ctx.Done():
			r.Close()
			return nil
		case err := <-notifyClose:
			return err
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("deliveries channel closed")
			}
			var e clickedEvent
			if err := json.NewDecoder(bytes.NewReader(d.Body)).Decode(&e); err != nil {
				r.log.Warn("bad message skipped", "err", err)
			} else {
				handle(ctx, e.Code)
			}
			if err := d.Ack(false); err != nil {
				r.log.Warn("ack failed", "err", err)
			}
		}
	}
}
