#!/usr/bin/env bash
# Этап 4: демонстрация, что произойдёт при запрещении ЛЕГИТИМНОГО правила.
# Отключаем api от сегмента данных — приложение должно сломаться
# (ошибки БД в /healthz и в операциях записи), затем возвращаем всё обратно.
source demo/lib.sh

step "Исходное состояние: всё здорово"
expect_ok "healthz = 200" curl -sf localhost:8080/healthz
info "$(curl -s localhost:8080/healthz)"

step "ЗАПРЕЩАЕМ легитимное правило: api -> data"
docker network disconnect shortlink_data shortlink-api
info "api отключён от shortlink_data"
sleep 3

step "Приложение падает: healthz = 503, запись невозможна"
CODE_HTTP=$(curl -s -o /tmp/hz.json -w '%{http_code}' localhost:8080/healthz)
info "healthz HTTP $CODE_HTTP: $(cat /tmp/hz.json)"
if [ "$CODE_HTTP" = "503" ]; then pass "healthz корректно сообщает об отказе (503)"; else fail "ожидали 503, получили $CODE_HTTP"; fi
CREATE_HTTP=$(curl -s -o /tmp/err.json -w '%{http_code}' -X POST localhost:8080/api/links \
    -H 'Content-Type: application/json' -d '{"url":"https://example.com"}')
info "POST /api/links HTTP $CREATE_HTTP: $(cat /tmp/err.json)"
if [ "$CREATE_HTTP" = "500" ]; then pass "запись в БД не проходит — правило заблокировано"; else fail "ожидали 500, получили $CREATE_HTTP"; fi

step "ВОЗВРАЩАЕМ правило: api -> data"
docker network connect shortlink_data shortlink-api
info "api снова подключён к shortlink_data, перезапускаем для чистых соединений"
compose restart api >/dev/null
for _ in $(seq 1 30); do curl -sf localhost:8080/healthz >/dev/null 2>&1 && break; sleep 2; done
expect_ok "healthz снова 200" curl -sf localhost:8080/healthz
expect_ok "запись снова работает" curl -sf -X POST localhost:8080/api/links -H 'Content-Type: application/json' -d '{"url":"https://example.com"}'

summary
