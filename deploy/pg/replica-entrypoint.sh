#!/bin/sh
# Точка входа standby-узла PostgreSQL: при первом старте снимает базовую
# копию с primary (pg_basebackup) и стартует в режиме горячего standby.
set -eu

if [ ! -s "$PGDATA/PG_VERSION" ]; then
    echo "bootstrapping replica from $PRIMARY_HOST ..."
    mkdir -p "$PGDATA"
    chmod 700 "$PGDATA"
    gosu postgres env PGPASSWORD="$REPL_PASSWORD" pg_basebackup \
        -h "$PRIMARY_HOST" -p 5432 -U "$REPL_USER" \
        -D "$PGDATA" -Fp -Xs -R -C -S "${PGSLOT:-replica_1}"
    cat >> "$PGDATA/postgresql.auto.conf" <<EOF
primary_conninfo = 'host=$PRIMARY_HOST port=5432 user=$REPL_USER password=$REPL_PASSWORD'
hot_standby = on
EOF
    chown -R postgres:postgres "$PGDATA"
fi

exec gosu postgres postgres -D "$PGDATA"
