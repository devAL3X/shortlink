# deploy/ansible — деплой shortlink на VDS

Файлы **не коммитятся** (в репозитории их нет, `.gitignore` не трогаем — просто не добавляем).

## Что делает `site.yml` (Debian 13, root по ключу)

1. **Docker** из официального репозитория + compose-плагин
2. **Код** — git clone `devAL3X/shortlink` (main) в `/opt/shortlink`
3. **docker-compose.override.yml** — порты 8080 и 15672 привязаны к `127.0.0.1`.
   Это ключевой момент безопасности: docker публикует порты через свои iptables-правила
   **в обход ufw**, поэтому привязка к loopback — единственная надёжная защита.
4. **nginx** — единственная точка входа (:80), basic auth, security-заголовки,
   `server_tokens off`, проксирование на 127.0.0.1:8080
5. **sshd** — дропин `00-hardening.conf`: `PasswordAuthentication no`,
   `KbdInteractiveAuthentication no`, `PermitRootLogin prohibit-password`
6. **ufw** — разрешены только ssh и http, вход по умолчанию deny
7. **Самопроверка** — приложение на loopback, без пароля 401, с паролем 200

## Доступ

- Приложение: `http://195.133.13.220/` — логин `shortlink`, пароль в `group_vars/secrets.yml`
- RabbitMQ management UI: только с сервера — `ssh -L 15672:127.0.0.1:15672 root@195.133.13.220`
- БД/кэш/брокер наружу не опубликованы вовсе

## Секреты

Пароль и хэш лежат в `group_vars/secrets.yml` — файл прописан в `.gitignore`
репозитория и в коммит не попадёт. В `group_vars/all.yml` только публичные настройки.

Сменить пароль: `openssl passwd -apr1 'новый'` → обнови `basic_auth_hash`
и `basic_auth_password` в `secrets.yml` → перезапусти плейбук.

Хочешь хранить секреты в репозитории — зашифруй файл ansible-vault:

```sh
~/.venvs/ansible/bin/ansible-vault encrypt group_vars/secrets.yml
# запускать с --ask-vault-pass
```

## Запуск

```sh
~/.venvs/ansible/bin/ansible-playbook site.yml          # из deploy/ansible
~/.venvs/ansible/bin/ansible-playbook site.yml --check  # dry-run (apt/docker частично пропустит)
```

## Обновление приложения

Повторный запуск плейбука: git pull → `docker compose up -d --build` (идемпотентно,
без изменений контейнеры не пересоздаются).

## Чего тут нет (осознанно)

- TLS/https: домена нет; когда появится — certbot + listen 443
- TLS между микросервисами: пока http внутри docker-сетей (роадмап проекта)
