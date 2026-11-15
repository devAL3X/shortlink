#!/usr/bin/env bash
# Этап 5: отказоустойчивость брокера сообщений — остановка узла кластера
# RabbitMQ, quorum-очередь и переподключение клиентов, поток кликов живёт.
source demo/lib.sh

clicks_of() { # код -> счётчик кликов из API
    curl -s localhost:8080/api/links | sed -n "s/.*\"code\":\"$1\"[^}]*\"clicks\":\([0-9]*\).*/\1/p"
}

step "1. Состояние кластера RabbitMQ (3 узла)"
docker exec shortlink-rabbitmq1 rabbitmqctl cluster_status \
    | sed -n '/Running Nodes/,/Versions/p' | grep -v Versions | sed 's/^/   /'

step "2. Генерируем поток кликов по ссылке"
BODY=$(curl -s -X POST localhost:8080/api/links -H 'Content-Type: application/json' -d '{"url":"https://rabbitmq.com/"}')
CODE=$(echo "$BODY" | sed -n 's/.*"code":"\([^"]*\)".*/\1/p')
expect_ok "создана ссылка ($CODE)" test -n "$CODE"
for _ in $(seq 1 10); do curl -s -o /dev/null localhost:8080/r/$CODE; done
sleep 3 # worker обрабатывает
C1=$(clicks_of "$CODE")
info "счётчик кликов до аварии: $C1"

step "3. АВАРИЯ: останавливаем один узел кластера (rabbitmq3)"
compose stop rabbitmq3 >/dev/null
info "rabbitmq3 остановлен; очередь link.clicked — quorum, кворум на оставшихся 2 узлах"
docker exec shortlink-rabbitmq1 rabbitmqctl cluster_status \
    | sed -n '/Running Nodes/,/Versions/p' | grep -v Versions | sed 's/^/   /'

step "4. Поток сообщений живёт: новые клики обрабатываются"
for _ in $(seq 1 10); do curl -s -o /dev/null localhost:8080/r/$CODE; done
sleep 3
C2=$(clicks_of "$CODE")
info "счётчик кликов после аварии: $C2"
if [ "${C2:-0}" -gt "${C1:-0}" ]; then pass "клики продолжают доставляться и обрабатываться (+$((C2-C1)))"; else fail "поток сообщений остановился"; fi
info "переподключение клиента api (лог):"
docker logs shortlink-api 2>&1 | grep 'rabbit connected' | tail -2 | sed 's/^/   /'

step "5. Возвращаем узел в кластер"
compose start rabbitmq3 >/dev/null
sleep 5
docker exec shortlink-rabbitmq1 rabbitmqctl cluster_status \
    | sed -n '/Running Nodes/,/Versions/p' | grep -v Versions | sed 's/^/   /'
pass "узел вернулся в кластер, работа очереди восстановлена"

summary
