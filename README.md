# TempLink Xray

Временный доступ в интернет через **VLESS + Xray + WebSocket**.

Проект состоит из двух сервисов:

* **templink-xray** — Xray Core, принимает VLESS-подключения.
* **templink-web** — web-интерфейс для создания временного доступа, VLESS-ссылки и QR-кода.

По умолчанию доступ действует **10 минут**.

HTTPS и внешний доступ организуются через **Nginx Proxy Manager (NPM)**.

---

## Структура

```text
templink-xray/
├── docker-compose.yml
├── .env
└── xray/
    └── config.json
```

`templink-web` используется как готовый Docker-образ:

```text
comradekit66/templink-web:1.0.0
```

Конфигурация Xray находится в:

```text
xray/config.json
```

> При переносе проекта на другой сервер проверьте наличие актуального `config.json`.
>
> Если web-интерфейс или `index.html` используются из локальных файлов через volume, их также может потребоваться скопировать вручную.

---

# Архитектура

```text
                         Internet
                            │
                            ▼
                 ┌─────────────────────┐
                 │ Nginx Proxy Manager  │
                 │     HTTPS / TLS     │
                 └──────────┬──────────┘
                            │
                 ┌──────────┴──────────┐
                 │                     │
                 ▼                     ▼
        tempcode.example.com   templink.example.com
                 │                     │
                 ▼                     ▼
        templink-web :3002       Xray :10000
                                       │
                                  WebSocket /xray
                                       │
                                       ▼
                                    Internet

                    templink-web
                         │
                         │ Xray API
                         ▼
                    127.0.0.1:10085
```

---

# Порты

|    Порт | Назначение          | Доступ          |
| ------: | ------------------- | --------------- |
|  `3002` | Web-интерфейс и API | через NPM       |
| `10000` | VLESS + WebSocket   | через NPM       |
| `10085` | Xray Handler API    | только локально |

---

# `.env`

`.env` используется для настройки `templink-web`.

Пример:

```env
WEB_ADDR=0.0.0.0
WEB_PORT=3002

XRAY_API_ADDR=127.0.0.1
XRAY_API_PORT=10085

ACCESS_TTL_MINUTES=10
```

Конфигурация самого Xray находится отдельно:

```text
xray/config.json
```

---

# Nginx Proxy Manager

## Web-интерфейс

Создать Proxy Host:

```text
Domain Names:
tempcode.example.com

Scheme:
http

Forward Hostname / IP:
SERVER_IP

Forward Port:
3002
```

В SSL:

```text
Request a new SSL Certificate
Force SSL
```

---

## VLESS WebSocket

Создать отдельный Proxy Host:

```text
Domain Names:
templink.example.com

Scheme:
http

Forward Hostname / IP:
SERVER_IP

Forward Port:
10000
```

Включить:

```text
Websockets Support: ON
```

В SSL:

```text
Request a new SSL Certificate
Force SSL
```

### Advanced

В поле **Advanced** добавить:

```nginx
location /xray {
    proxy_pass http://SERVER_IP:10000;

    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";

    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;

    proxy_read_timeout 86400;
    proxy_send_timeout 86400;
}
```

Заменить:

```text
SERVER_IP
```

на IP сервера.

### Важно

Эти строки необходимы для работы WebSocket:

```nginx
proxy_http_version 1.1;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "upgrade";
```

WebSocket path должен совпадать с Xray:

```text
/xray
```

TLS завершается на Nginx Proxy Manager. Сам Xray работает без TLS.

---

# Запуск

## Весь стек

```bash
docker compose up -d
```

Если требуется пересборка Xray:

```bash
docker compose up -d --build
```

---

## Только Xray

```bash
docker compose up -d templink-xray
```

С пересборкой:

```bash
docker compose up -d --build templink-xray
```

---

## Только Web

`templink-web` используется из готового образа:

```text
comradekit66/templink-web:1.0.0
```

Запуск:

```bash
docker compose up -d templink-web
```

Образ будет автоматически загружен Docker, если его нет локально.

---

# Проверка

Проверить состояние контейнеров:

```bash
docker compose ps
```

Проверить открытые порты:

```bash
ss -lntp | grep -E '3002|10000|10085'
```

Ожидается:

```text
0.0.0.0:3002
0.0.0.0:10000
127.0.0.1:10085
```

Проверить Web:

```bash
curl http://127.0.0.1:3002/
```

Получить текущий доступ:

```bash
curl http://127.0.0.1:3002/api/access
```

---

# Логи

Логи всего стека:

```bash
docker compose logs -f
```

Логи Xray:

```bash
docker logs -f templink-xray
```

Логи Web:

```bash
docker logs -f templink-web
```

Последние 50 строк Xray:

```bash
docker logs --tail 50 templink-xray
```

---

# Как работает доступ

1. Пользователь открывает web-страницу.
2. `templink-web` проверяет наличие действующего доступа.
3. Если доступ ещё действует — возвращается тот же UUID.
4. После окончания срока создаётся новый UUID.
5. UUID добавляется в Xray через Handler API.
6. Пользователь получает VLESS-ссылку и QR-код.
7. Клиент подключается через VLESS → WebSocket → Xray.

---

# Конфигурация Xray

Основной файл:

```text
xray/config.json
```

Основные параметры:

```text
VLESS:
10000

WebSocket:
 /xray

Xray API:
10085
```

API доступен только локально:

```text
127.0.0.1:10085
```

VLESS inbound:

```text
0.0.0.0:10000
```

---

# Важный нюанс

Текущий активный доступ хранится в памяти `templink-web`.

При перезапуске `templink-web` Xray может сохранить ранее добавленного динамического пользователя `temporary`.

В этом случае web-сервис может получить ошибку:

```text
User temporary already exists
```

Для очистки динамического состояния Xray:

```bash
docker restart templink-xray
```

После этого необходимо получить новый доступ:

```bash
curl http://127.0.0.1:3002/api/access
```

---

# Перезапуск после изменений

После изменения `xray/config.json`:

```bash
docker compose restart templink-xray
```

Если Xray собирается из Dockerfile и требуется пересборка:

```bash
docker compose up -d --build templink-xray
```

После изменения параметров `templink-web`:

```bash
docker compose restart templink-web
```

После изменения `docker-compose.yml`:

```bash
docker compose up -d
```

---

# Полная пересборка

Если необходимо пересоздать оба контейнера:

```bash
docker compose down
```

```bash
docker compose up -d --build
```

Проверить результат:

```bash
docker compose ps
```

---

# Быстрая проверка после установки

Проверить контейнеры:

```bash
docker compose ps
```

Проверить web:

```bash
curl http://127.0.0.1:3002/
```

Проверить выдачу доступа:

```bash
curl http://127.0.0.1:3002/api/access
```

Проверить Xray:

```bash
docker logs --tail 50 templink-xray
```

При успешном подключении клиента в логах Xray появляются строки вида:

```text
accepted tcp:...
```

Это означает, что VLESS-подключение прошло через Xray и трафик передаётся.

---

## Docker image

Web-сервис:

```text
comradekit66/templink-web:1.0.0
```

Xray:

```text
ghcr.io/xtls/xray-core:26.9.9
```
