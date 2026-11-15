#!/usr/bin/env bash
# Этапы 2–3: сборка, деплой и smoke-проверка минимального сценария.
#   hello-world, CRUD в PostgreSQL, кэш Redis, брокер RabbitMQ.
source demo/lib.sh

wait_healthy() { # ждём HTTP 200 от /healthz (503 пока компоненты поднимаются)
    for _ in $(seq 1 90); do
        if curl -sf localhost:8080/healthz >/dev/null 2>&1; then return 0; fi
        sleep 2
    done
    return 1
}

step "Сборка и запуск (docker compose up -d --build)"
unset DATABASE_URL # на случай, если осталась после demo/db_failover.sh
compose down -v >/dev/null 2>&1 || true
compose up -d --build >/dev/null
info "ожидаем готовности api (healthz)..."
wait_healthy
expect_ok "приложение поднято: /healthz = 200" curl -sf localhost:8080/healthz

step "Этап 2: hello-world микросервис"
info "ответ: $(curl -s localhost:8080/hello)"
expect_ok "GET /hello отвечает" curl -sf localhost:8080/hello
info "docker compose ps:"
compose ps --format 'table {{.Name}}\t{{.Service}}\t{{.Status}}' | sed 's/^/   /'

step "Этап 3: INSERT — создаём ссылку"
BODY=$(curl -s -X POST localhost:8080/api/links -H 'Content-Type: application/json' -d '{"url":"https://www.hse.ru/"}')
info "ответ api: $BODY"
CODE=$(echo "$BODY" | sed -n 's/.*"code":"\([^"]*\)".*/\1/p')
expect_ok "код ссылки получен ($CODE)" test -n "$CODE"

step "SELECT — список ссылок (из PostgreSQL)"
curl -s localhost:8080/api/links | sed 's/^/   /'

step "Кэш Redis: первый переход — MISS (мимо кэша), второй — HIT"
H1=$(curl -si localhost:8080/r/$CODE | tr -d '\r' | sed -n 's/[Xx]-[Cc]ache: //p' | head -1)
H2=$(curl -si localhost:8080/r/$CODE | tr -d '\r' | sed -n 's/[Xx]-[Cc]ache: //p' | head -1)
info "переход 1: X-Cache=$H1  переход 2: X-Cache=$H2"
if [ "$H1" = "MISS" ]; then pass "первый ответ взят из БД (MISS)"; else fail "ожидали MISS, получили '$H1'"; fi
if [ "$H2" = "HIT" ]; then pass "второй ответ взят из Redis (HIT)"; else fail "ожидали HIT, получили '$H2'"; fi
info "ключ в redis:"
docker exec shortlink-redis redis-cli --scan --pattern 'link:*' | sed 's/^/     /'
info "TTL ключа: $(docker exec shortlink-redis redis-cli TTL "link:$CODE") сек"

step "Брокер RabbitMQ: события кликов обработаны worker'ом"
sleep 3
CLICKS=$(curl -s localhost:8080/api/links | sed -n 's/.*"clicks":\([0-9]*\).*/\1/p' | head -1)
info "переходов сделано: 2, счётчик в БД: $CLICKS"
if [ "${CLICKS:-0}" -ge 2 ]; then pass "worker потребил сообщения из брокера и записал клики в БД"; else fail "клики не посчитаны"; fi
docker exec shortlink-rabbitmq1 rabbitmqctl list_queues name messages consumers \
    | grep link.clicked | sed 's/^/   очередь: /'

summary
