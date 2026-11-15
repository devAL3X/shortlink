#!/usr/bin/env bash
# Общие функции демо-скриптов: цветной вывод и проверка ожиданий.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
ROOT=$(pwd)

GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[0;33m'; CYAN='\033[0;36m'; BOLD='\033[1m'; OFF='\033[0m'

step() { echo -e "\n${CYAN}${BOLD}== $* ==${OFF}"; }
info() { echo -e "   $*"; }
pass() { echo -e "   ${GREEN}✔ $*${OFF}"; }
fail() { echo -e "   ${RED}✘ $*${OFF}"; }
note() { echo -e "   ${YELLOW}› $*${OFF}"; }

# expect_ok "описание" cmd...  — команда обязана завершиться успехом
expect_ok() {
    local desc=$1; shift
    if "$@" >/dev/null 2>&1; then pass "$desc"; else fail "$desc"; FAILURES=$((FAILURES+1)); fi
}

# expect_fail "описание" cmd... — команда обязана завершиться ошибкой (запрещённое действие)
expect_fail() {
    local desc=$1; shift
    if "$@" >/dev/null 2>&1; then fail "$desc (соединение прошло, а не должно было)"; FAILURES=$((FAILURES+1))
    else pass "$desc (отклонено, как и ожидалось)"; fi
}

tcp_check() { # host port — проверка TCP-доступности изнутри контейнера
    local host=$1 port=$2
    nc -w 2 "$host" "$port" </dev/null
}

summary() {
    echo -e ""
    if [ "${FAILURES:-0}" -eq 0 ]; then
        echo -e "${GREEN}${BOLD}ИТОГ: все проверки пройдены${OFF}"
    else
        echo -e "${RED}${BOLD}ИТОГ: ошибок — $FAILURES${OFF}"
        exit 1
    fi
}

compose() { docker compose "$@"; }

FAILURES=0
