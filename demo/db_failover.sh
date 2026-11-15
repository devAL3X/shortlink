#!/usr/bin/env bash
# Этап 5: отказоустойчивость СУБД — некорректное завершение master,
# переключение (promote) на standby, приложение продолжает работать.
source demo/lib.sh

clicks_of() { # код -> счётчик кликов из API
    curl -s localhost:8080/api/links | sed -n "s/.*\"code\":\"$1\"[^}]*\"clicks\":\([0-9]*\).*/\1/p"
}

step "1. Исходное состояние: пишем в primary, реплика догоняет"
BODY=$(curl -s -X POST localhost:8080/api/links -H 'Content-Type: application/json' -d '{"url":"https://github.com/"}')
CODE=$(echo "$BODY" | sed -n 's/.*"code":"\([^"]*\)".*/\1/p')
expect_ok "создана ссылка ($CODE)" test -n "$CODE"
P_COUNT=$(docker exec shortlink-postgres-primary psql -U shortlink -tAc 'select count(*) from links')
sleep 2 # асинхронная репликация
R_COUNT=$(docker exec shortlink-postgres-replica psql -U shortlink -tAc 'select count(*) from links')
info "primary: $P_COUNT ссылок, replica: $R_COUNT ссылок"
if [ "$P_COUNT" = "$R_COUNT" ]; then pass "реплика синхронизирована"; else fail "реплика отстаёт ($P_COUNT vs $R_COUNT)"; fi
info "канал репликации на primary:"
docker exec shortlink-postgres-primary psql -U shortlink -c 'select application_name, state, sync_state from pg_stat_replication' | sed 's/^/   /'

step "2. АВАРИЯ: некорректное завершение master (docker compose stop postgres-primary)"
T0=$(date +%s)
compose stop postgres-primary >/dev/null
info "primary остановлен"
sleep 2
HZ=$(curl -s -o /dev/null -w '%{http_code}' localhost:8080/healthz)
info "healthz api: HTTP $HZ (postgres недоступен)"
if [ "$HZ" = "503" ]; then pass "api корректно видит отказ СУБД"; else fail "ожидали 503, получили $HZ"; fi

step "3. ПЕРЕКЛЮЧЕНИЕ: promote standby в новый master"
docker exec shortlink-postgres-replica gosu postgres pg_ctl promote -D /var/lib/postgresql/data >/dev/null
IN_REC=$(docker exec shortlink-postgres-replica psql -U shortlink -tAc 'select pg_is_in_recovery()')
if [ "$IN_REC" = "f" ]; then pass "реплика повышена до read-write (новый master)"; else fail "promote не сработал"; fi

step "4. Приложение переключено на новый master (DATABASE_URL -> postgres-replica)"
export DATABASE_URL='postgres://shortlink:shortlink@postgres-replica:5432/shortlink'
compose up -d --force-recreate api worker >/dev/null 2>&1
for _ in $(seq 1 60); do curl -sf localhost:8080/healthz >/dev/null 2>&1 && break; sleep 2; done
expect_ok "healthz = 200 на новом master" curl -sf localhost:8080/healthz
T1=$(date +%s)
note "RTO переключения: ~$((T1-T0)) сек (обнаружение + promote + рестарт сервисов)"

step "5. Приложение снова пишет и читает (уже на новом master)"
BODY2=$(curl -s -X POST localhost:8080/api/links -H 'Content-Type: application/json' -d '{"url":"https://www.postgresql.org/"}')
CODE2=$(echo "$BODY2" | sed -n 's/.*"code":"\([^"]*\)".*/\1/p')
expect_ok "новая ссылка создана на standby ($CODE2)" test -n "$CODE2"
N_COUNT=$(docker exec shortlink-postgres-replica psql -U shortlink -tAc 'select count(*) from links')
info "ссылок на новом master: $N_COUNT (было $P_COUNT до аварии + 1 новая)"
if [ "$N_COUNT" -eq $((P_COUNT+1)) ]; then pass "данные пережили аварию, записи работают"; else fail "расхождение данных ($N_COUNT)"; fi
note "возврат в исходное состояние: unset DATABASE_URL && ./demo/run.sh"

summary
