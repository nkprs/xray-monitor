# Xray VPN monitoring stack

Репозиторий поднимает базовый стек мониторинга для Xray VPN.

Текущий профиль настроен консервативно под маленький VPS с `1 GB RAM`: на сервере крутятся только `Prometheus + exporters`, а `Grafana` запускается локально через SSH-туннель.

Ниже сценарий ориентирован на установку Xray через `x-ui`, где живой конфиг лежит в `/usr/local/x-ui/bin/config.json`.

- `xray-exporter` для Xray API и `access.log`
- `Prometheus` для хранения метрик минимум 2 дня
- `Grafana` в отдельном локальном compose
- `node-exporter` для CPU, RAM, диска и сети VPS
- `xraycfg` на Go для генерации и проверки обязательных секций Xray-конфига

## Что создаётся

- на сервере наружу не публикуется ничего, кроме SSH
- Prometheus открывается только на `127.0.0.1:9090`
- `xray-exporter` и `node-exporter` слушают только loopback (`127.0.0.1`) на хосте
- данные Prometheus и локальной Grafana хранятся в docker volumes

## Структура

- `docker-compose.yml` — серверный стек `Prometheus + exporters`
- `docker-compose.grafana.yml` — локальная Grafana
- `prometheus/prometheus.yml` — scrape-конфиг
- `grafana/provisioning` — автоподключение datasource/dashboard
- `grafana/dashboards/xray-monitoring.json` — стартовый dashboard
- `cmd/xraycfg` — CLI для Xray-конфига
- `xray/xray-required.json` — готовый JSON-фрагмент для Xray

## 1. Сборка бинарника для Ubuntu 24

Лучший вариант: собирать бинарник локально и копировать его на сервер. Для этого здесь уже есть `Makefile`.

Сборка под обычный VPS на `x86_64`:

```bash
make build-linux-amd64
```

Сборка сразу двух вариантов:

```bash
make build-linux
```

Результат:

```text
dist/xraycfg-linux-amd64
dist/xraycfg-linux-arm64
```

Почему так лучше для Ubuntu 24:

- `CGO_ENABLED=0` даёт самодостаточный Go-бинарник без привязки к системной `glibc`
- сборка идёт на локальной машине, на сервере нужен только сам файл
- меньше подвижных частей, чем у `go run`

Проверить архитектуру сервера перед копированием:

```bash
uname -m
```

Обычно:

- `x86_64` => `dist/xraycfg-linux-amd64`
- `aarch64` => `dist/xraycfg-linux-arm64`

Копирование на сервер:

```bash
scp dist/xraycfg-linux-amd64 user@server:/usr/local/bin/xraycfg
ssh user@server 'chmod +x /usr/local/bin/xraycfg'
```

Проверка на сервере:

```bash
/usr/local/bin/xraycfg version
```

## 2. Подготовить Xray

Если у тебя уже есть рабочий `config.json`, можно слить в него обязательные секции автоматически.

Важно:

- существующие клиентские inbounds и их порты не переписываются
- текущий `api` inbound сохраняется, если он уже есть
- `xraycfg merge` добавляет недостающие секции мониторинга и включает логи
- для `x-ui` нельзя полагаться на ручное редактирование `bin/config.json`: панель перегенерирует его при перезапуске

Для обычной установки Xray (без `x-ui`) можно применять merge напрямую:

```bash
xraycfg merge \
  --api-port 62789 \
  --in /usr/local/etc/xray/config.json \
  --out /usr/local/etc/xray/config.monitoring.json
```

Для `x-ui` на этом сервере текущий API уже слушает `127.0.0.1:62789`.
Поэтому фактически нужно:

```bash
xraycfg validate --file /usr/local/x-ui/bin/config.json
```

И включить `access/error` логи через web-панель `x-ui` (раздел настроек Xray), после чего выполнить `Restart Xray` из панели.
`metrics`-блок `x-ui` обычно не сохраняет в сгенерированном конфиге, и это ожидаемо для текущего стека: `xray-exporter` использует API + access.log.

Если нужен только шаблон фрагмента:

```bash
xraycfg patch
```

Проверка существующего конфига:

```bash
xraycfg validate --file /usr/local/x-ui/bin/config.json
```

Строгая проверка с обязательным `metrics` (для plain Xray):

```bash
xraycfg validate --require-metrics --file /usr/local/etc/xray/config.json
```

CLI проверяет и/или добавляет:

- `stats`
- `api` c `StatsService`
- localhost inbound для API (в этом проекте обычно `127.0.0.1:62789`)
- routing rule для `api`
- `policy.levels.0` и `policy.system`
- `metrics.tag`
- `metrics.listen`
- `log.access` и `log.error`

После слияния секций перезапусти Xray и проверь API вручную. Пример с установленным `xray`:

```bash
xray api statsquery --server=127.0.0.1:62789 -pattern 'user>>>'
```

## 3. Поднять серверный стек мониторинга

Скопируй env-файл:

```bash
cp .env.example .env
```

Минимум проверь:

- `XRAY_API_ENDPOINT=127.0.0.1:62789`
- `XRAY_ACCESS_LOG=/var/log/xray/access.log`
- `XRAY_ACCESS_LOG_DIR=/var/log/xray`
- `PROMETHEUS_LISTEN_ADDRESS=127.0.0.1:9090`
- `PROMETHEUS_RETENTION=2d`
- `PROMETHEUS_MEM_LIMIT=256m`

Важно: в compose используется локальная сборка образа `xray-exporter` из исходников тега `XRAY_EXPORTER_VERSION` (по умолчанию `v0.2.0`) с inline-патчем в `docker/xray-exporter/Dockerfile`.
Это решает два практических момента:
- обход `permission denied` у готового `ghcr.io/compassvpn/xray-exporter:latest` на части хостов;
- экспорт user-series `xray_traffic_*{dimension="user",target="<email>"}` для анализа распределения throughput по клиентам.

Старт:

```bash
docker compose up -d
```

Профиль по памяти по умолчанию:

- `Prometheus`: `256 MB`
- `xray-exporter`: `64 MB`
- `node-exporter`: `64 MB`

Это не гарантия точного потребления, а верхние лимиты контейнеров.

## 4. Проверка серверной части

Проверить контейнеры:

```bash
docker compose ps
```

Проверить, что Prometheus видит targets:

```bash
docker compose exec prometheus wget -qO- http://localhost:9090/api/v1/targets
```

Проверить метрики exporter:

```bash
docker compose exec prometheus wget -qO- http://127.0.0.1:9550/scrape
```

Проверить, что Prometheus слушает только loopback на сервере:

```bash
ss -ltnp | grep 9090
```

Ожидаемо увидеть `127.0.0.1:9090`.

## 5. Поднять локальную Grafana

На локальной машине открой SSH-туннель до Prometheus:

```bash
ssh -N -L 9090:127.0.0.1:9090 441
```

Потом в отдельном окне терминала запусти локальную Grafana:

```bash
docker compose -f docker-compose.grafana.yml up -d
```

По умолчанию datasource уже смотрит в:

```text
http://host.docker.internal:9090
```

На macOS это корректно для Grafana в Docker, если SSH-туннель поднят на локальной машине.

Grafana будет доступна по адресу:

```text
http://127.0.0.1:3000
```

Логин и пароль берутся из `.env`.

## 6. Что уже есть в Grafana

Dashboard `Xray VPN Monitoring` включает:

- активных пользователей
- uplink/downlink по пользователям
- топ пользователей по трафику
- inbound/outbound totals
- CPU и memory VPS
- сетевой throughput хоста
- usage корневого диска

## 7. Безопасность

На сервере достаточно оставить доступным только SSH. Порт `9090` слушает на `127.0.0.1`, поэтому наружу он не торчит, но firewall всё равно стоит держать закрытым.

Пример для UFW:

```bash
ufw allow OpenSSH
ufw deny 9090/tcp
ufw deny 9100/tcp
ufw deny 9550/tcp
ufw enable
```

## 8. Замечания

- `xray-exporter`, `prometheus` и `node-exporter` запущены в `network_mode: host`, чтобы безопасно работать с API Xray на `127.0.0.1`.
- Для текущего сервера с `x-ui` API уже живёт на `127.0.0.1:62789`, поэтому в `.env` нужно использовать именно этот порт.
- Для `x-ui` в текущей версии нормально, если `metrics` отсутствует в `bin/config.json`: `xray-exporter` продолжает работать по API и access.log.
- В этой конфигурации `xray-exporter` собран с включённым user traffic (label `dimension="user"`), поэтому панели по пользователям в Grafana работают напрямую по Xray API stats.
- User-series увеличивают кардинальность метрик: при большом числе клиентов может потребоваться уменьшить retention или поднять лимит памяти Prometheus.
- Для `1 GB RAM` лучше иметь хотя бы `1 GB` swap. На Ubuntu 24 это можно сделать через `fallocate`, `mkswap`, `swapon` и запись в `/etc/fstab`.
