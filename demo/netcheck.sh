#!/usr/bin/env bash
# Этап 4: сегментация сетей и правила firewall.
# Показываем, что разрешённые связи работают, а запрещённые — нет.
source demo/lib.sh

step "Топология сегментов (docker network inspect)"
for net in edge app data; do
    info "shortlink_$net: $(docker network inspect shortlink_$net --format '{{range .Containers}}{{.Name}} {{end}}')"
done

step "РАЗРЕШЕНО: frontend -> api (сегмент app)"
expect_ok "frontend может обращаться к api" \
    docker exec shortlink-frontend wget -q -O- -T 3 http://api:8080/hello

step "РАЗРЕШЕНО: api -> PostgreSQL / Redis / RabbitMQ (сегмент data)"
expect_ok "api -> postgres-primary:5432" docker exec shortlink-api sh -c 'nc -w 2 postgres-primary 5432 </dev/null'
expect_ok "api -> redis:6379" docker exec shortlink-api sh -c 'nc -w 2 redis 6379 </dev/null'
expect_ok "api -> rabbitmq1:5672" docker exec shortlink-api sh -c 'nc -w 2 rabbitmq1 5672 </dev/null'

step "РАЗРЕШЕНО: worker -> RabbitMQ / PostgreSQL (сегмент data)"
expect_ok "worker -> rabbitmq2:5672" docker exec shortlink-worker sh -c 'nc -w 2 rabbitmq2 5672 </dev/null'
expect_ok "worker -> postgres-primary:5432" docker exec shortlink-worker sh -c 'nc -w 2 postgres-primary 5432 </dev/null'

step "ЗАПРЕЩЕНО: из сегмента edge к api и к сегменту данных"
# посторонний контейнер только в edge — как скомпрометированный узел внешнего контура
expect_fail "edge -> api:8080" docker run --rm --network shortlink_edge busybox nc -w 2 api 8080
expect_fail "edge -> postgres-primary:5432" docker run --rm --network shortlink_edge busybox nc -w 2 postgres-primary 5432
expect_fail "edge -> redis:6379" docker run --rm --network shortlink_edge busybox nc -w 2 redis 6379
expect_fail "edge -> rabbitmq1:5672" docker run --rm --network shortlink_edge busybox nc -w 2 rabbitmq1 5672

step "ЗАПРЕЩЕНО: из сегмента app напрямую к данным (мимо api)"
expect_fail "app -> postgres-primary:5432" docker run --rm --network shortlink_app busybox nc -w 2 postgres-primary 5432
expect_fail "app -> redis:6379" docker run --rm --network shortlink_app busybox nc -w 2 redis 6379
expect_fail "app -> rabbitmq1:5672" docker run --rm --network shortlink_app busybox nc -w 2 rabbitmq1 5672
expect_ok "app -> api:8080 (единственная разрешённая связь)" docker run --rm --network shortlink_app busybox nc -w 2 api 8080

step "ЗАПРЕЩЕНО: исходящий трафик из сегмента данных (internal network)"
expect_fail "data -> example.com:443 (интернет)" docker run --rm --network shortlink_data busybox nc -w 3 example.com 443

step "Наружу опубликованы только 8080 (frontend) и 15672 (rabbitmq management)"
docker ps --filter name=shortlink --format '   {{.Names}}: {{.Ports}}'

summary
