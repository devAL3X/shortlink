#!/usr/bin/env bash
# Этап 5: резервное копирование и восстановление СУБД.
# pg_dump -> DROP TABLE -> восстановление -> данные на месте.
source demo/lib.sh
mkdir -p backups

step "1. Состояние до резервного копирования"
P_BEFORE=$(docker exec shortlink-postgres-primary psql -U shortlink -tAc 'select count(*) from links')
info "ссылок в БД: $P_BEFORE"

step "2. Резервная копия (pg_dump, логический формат)"
docker exec shortlink-postgres-primary pg_dump -U shortlink shortlink > backups/demo_backup.sql
info "файл: backups/demo_backup.sql ($(wc -l < backups/demo_backup.sql) строк)"
grep -c '^COPY ' backups/demo_backup.sql | sed 's/^/   таблиц с данными: /'

step "3. КАТАСТРОФА: удаляем таблицы"
docker exec shortlink-postgres-primary psql -U shortlink -q -c 'DROP TABLE link_events; DROP TABLE links;'
info "таблицы links и link_events удалены"
curl -s localhost:8080/api/links | sed 's/^/   ответ api: /'

step "4. Восстановление из резервной копии"
docker exec -i shortlink-postgres-primary psql -U shortlink -q -d shortlink < backups/demo_backup.sql
info "копия применена"
P_AFTER=$(docker exec shortlink-postgres-primary psql -U shortlink -tAc 'select count(*) from links')
curl -s localhost:8080/api/links | head -c 300 | sed 's/^/   ответ api: /'; echo

step "5. Проверка"
if [ "$P_AFTER" -eq "$P_BEFORE" ]; then pass "данные восстановлены полностью ($P_AFTER ссылок)"; else fail "восстановлено $P_AFTER из $P_BEFORE"; fi
expect_ok "запись после восстановления работает" curl -sf -X POST localhost:8080/api/links -H 'Content-Type: application/json' -d '{"url":"https://www.postgresql.org/docs/backup/"}'

summary
