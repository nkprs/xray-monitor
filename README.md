# Xray VPN monitoring stack

Репозиторий поднимает базовый стек мониторинга для Xray VPN.

Текущий профиль настроен консервативно под маленький VPS с `1 GB RAM`: на сервере крутятся только `Prometheus + exporters`, а `Grafana` запускается локально через SSH-туннель.

- `xray-exporter` для Xray API и `access.log`
- `Prometheus` для хранения метрик минимум 2 дня
- `Grafana` в отдельном локальном compose
- `node-exporter` для CPU, RAM, диска и сети VPS
- `xraycfg` на Go для генерации и проверки обязательных секций Xray-конфига

## Что создаётся

- на сервере наружу не публикуется ничего, кроме SSH
- Prometheus открывается только на `127.0.0.1:9090`
- `xray-exporter` и `node-exporter` доступны только внутри docker-сети
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

Если у тебя уже есть рабочий `config.json`, можно слить в него обязательные секции автоматически:

```bash
xraycfg merge --in /usr/local/etc/xray/config.json --out /usr/local/etc/xray/config.monitoring.json
```

Если нужен только шаблон фрагмента:

```bash
xraycfg patch
```

Проверка существующего конфига:

```bash
xraycfg validate --file /usr/local/etc/xray/config.json
```

CLI проверяет и/или добавляет:

- `stats`
- `api` c `StatsService`
- localhost inbound на `127.0.0.1:10085`
- routing rule для `api`
- `policy.levels.0` и `policy.system`
- `metrics.listen`
- `log.access` и `log.error`

После слияния секций перезапусти Xray и проверь API вручную. Пример с установленным `xray`:

```bash
xray api statsquery --server=127.0.0.1:10085 -pattern 'user>>>'
```

## 3. Поднять серверный стек мониторинга

Скопируй env-файл:

```bash
cp .env.example .env
```

Минимум проверь:

- `XRAY_API_ENDPOINT=host.docker.internal:10085`
- `XRAY_ACCESS_LOG=/var/log/xray/access.log`
- `XRAY_ACCESS_LOG_DIR=/var/log/xray`
- `PROMETHEUS_BIND_ADDRESS=127.0.0.1`
- `PROMETHEUS_RETENTION=2d`
- `PROMETHEUS_MEM_LIMIT=256m`

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
docker compose exec prometheus wget -qO- http://xray-exporter:9550/scrape
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

- `xray-exporter` ожидает, что API Xray доступен на хосте по `127.0.0.1:10085`; контейнер ходит к нему через `host.docker.internal`.
- Метрики пользователя и активность зависят от того, какие именно series экспортирует образ `ghcr.io/compassvpn/xray-exporter`. Dashboard уже использует наиболее типичные имена метрик этого exporter.
- Если на VPS нет `host-gateway` поддержки в Docker, проще всего заменить `XRAY_API_ENDPOINT` на реальный IP хоста в bridge-сети или перевести exporter в `network_mode: host`.
- Для `1 GB RAM` лучше иметь хотя бы `1 GB` swap. На Ubuntu 24 это можно сделать через `fallocate`, `mkswap`, `swapon` и запись в `/etc/fstab`.
