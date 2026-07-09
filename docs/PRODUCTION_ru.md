# Руководство по развёртыванию в продакшене — netmon

> **Назначение:** Production-развёртывание **netmon** для мониторинга потерь TCP-пакетов (ретрансмиты) с разметкой IP по ролям/локациям.
>
> **Текущая версия:** Использует eBPF-треспоинт `tcp_retransmit_skb` как источник данных по умолчанию (production-ready с v1.0).

---

## Содержание

1. [Требования к системе](#1-требования-к-системе)
2. [Конфигурация](#2-конфигурация)
3. [systemd-сервис](#3-systemd-сервис)
4. [Health & Readiness](#4-health--readiness)
5. [Метрики и алерты](#5-метрики-и-алерты)
6. [Ограничения](#6-ограничения)

---

## 1. Требования к системе

### Требования к ядру

| Компонент | Требование | Примечания |
|-----------|------------|------------|
| **ОС** | Linux | x86_64 или arm64 |
| **Ядро** | **5.8+ минимум**, проверено на 5.15 / 6.1 / 6.8 / 6.12 | Ring buffer (`BPF_MAP_TYPE_RINGBUF`) требует 5.8+. На `<5.8` eBPF-путь недоступен — используйте `loss_source: tracepipe` |
| **BTF** | Файл `/sys/kernel/btf/vmlinux` должен существовать | CO-RE требует информации о BTF ядра |

**Проверка BTF:**
```bash
ls -la /sys/kernel/btf/vmlinux
# Должна вывести информацию о файле, а не "No such file"
```

### Необходимые capabilities

netmon использует eBPF для сбора потерь TCP. Требуются следующие capabilities:

| Capability | Назначение | Примечания |
|------------|------------|------------|
| `CAP_SYS_ADMIN` | Загрузка eBPF-программы **и** attach трейспоинта `tcp_retransmit_skb` | **Рекомендуется.** Покрывает весь путь eBPF + perf на всех проверенных ядрах |
| `CAP_NET_RAW` | Traceroute (ICMP/UDP/TCP), только если включён discovery | — |
| `CAP_BPF` + `CAP_PERFMON` | Least-privilege альтернатива CAP_SYS_ADMIN | Ядро 5.8+. **Может не сработать** — см. предупреждение ниже |

**Рекомендуемая конфигурация systemd (проверено на 5.15 / 6.1 / 6.8 / 6.12):**
```ini
AmbientCapabilities=CAP_SYS_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_SYS_ADMIN CAP_NET_RAW
```

**Least-privilege альтернатива (сначала проверьте на своём ядре):**
```ini
AmbientCapabilities=CAP_BPF CAP_PERFMON CAP_NET_RAW
CapabilityBoundingSet=CAP_BPF CAP_PERFMON CAP_NET_RAW
```

> ⚠️ **Почему не CAP_BPF + CAP_PERFMON по умолчанию?** Коллектор делает attach
> трейспоинта `tcp_retransmit_skb` через `perf_event_open`. На ядрах с
> `kernel.perf_event_paranoid > 1` (дефолт Debian/Ubuntu — 3/4) этот attach **не**
> разрешается набором CAP_BPF + CAP_PERFMON и падает с ошибкой
> `opening tracepoint perf event: permission denied`: программа *загружается*, но не
> attach'ится. `CAP_SYS_ADMIN` покрывает этот путь — поэтому рекомендуем его. Если
> нужен именно split-набор, проверьте что attach проходит на вашем ядре, либо
> понизьте порог: `sysctl kernel.perf_event_paranoid=1`.
>
> **Примечание:** systemd-юнит запускается с `User=root`, но CapabilityBoundingSet
> ограничивает возможности этого root-процесса — CAP_SYS_ADMIN здесь это capability
> для eBPF/perf, а не неограниченный root.

### Требования к файловой системе

| Путь | Назначение | Права |
|------|------------|-------|
| `/sys/kernel/btf/vmlinux` | Kernel BTF | Чтение |
| `/sys/kernel/tracing/trace_pipe` | Legacy fallback (опционально) | Чтение |
| `/etc/netmon/` | Директория конфигурации | Чтение |
| `/var/lib/netmon/` | Рабочая директория | Чтение/Запись |
| `/var/log/netmon/` | Файлы логов (если включено логирование в файл) | Запись |

---

## 2. Конфигурация

### Основной конфиг (config.yaml)

```yaml
global:
  # Источник данных о потерях TCP: "ebpf" (по умолчанию, production) или "tracepipe" (legacy/fallback)
  loss_source: ebpf
  
  # Адрес привязки HTTP-сервера метрик (должен быть валидным IP)
  metrics_addr: "0.0.0.0"
  
  # Порт HTTP-сервера метрик
  metrics_port: 9876
  
  # Опциональный токен аутентификации для эндпоинтов /metrics и /api/*
  # Можно также задать через переменную окружения NETMON_AUTH_TOKEN
  auth_token: ""  # Рекомендуется: задавать через env NETMON_AUTH_TOKEN
  
  # TTL для метрик в памяти (часы)
  ttl_hours: 3
  
  # Путь к trace_pipe (используется только если loss_source: tracepipe)
  trace_pipe_path: /sys/kernel/tracing/trace_pipe

metadata:
  locations:
    path: /etc/netmon/locations.yaml
    # Опциональное автообновление по HTTP:
    # update_source:
    #   url: https://config.example.com/locations.yaml
    #   poll_interval: 20m
    #   timeout: 10s
  roles:
    path: /etc/netmon/roles.yaml
  topology:
    path: /etc/netmon/topology.yaml

metrics:
  # Управление кардинальностью метрик потерь
  cardinality:
    # Level: "ip" | "role" | "network"
    #   - ip: каждый сериас с метками src_ip/dst_ip (неограниченно, НЕ рекомендуется для больших сетей)
    #   - network: агрегация по /24 сетям (без меток на каждый IP)
    #   - role: агрегация по location/role/vrf (без IP, без сети) [ПО УМОЛЧАНИЮ]
    level: role
    
    # Жёсткий лимит активных серий (0 = без ограничений)
    # Предотвращает OOM на Prometheus и самом netmon
    max_series: 10000

logging:
  level: info
  format: json
  # output_path: /var/log/netmon/netmon.log  # Пусто = stdout/stderr

discovery:
  traceroute:
    enabled: true
    mode: both  # both | top_loss | on_demand | periodic
    protocol: icmp  # icmp | udp | tcp
    interval: 5m
    top_n: 10
    max_hops: 30
    timeout: 3s
    probes_per_hop: 3
```

### ⚠️ Критично: уровень кардинальности

**Уровень по умолчанию `level: role` НЕ включает `src_ip`/`dst_ip` в метки метрик.**

Это сделано намеренно для предотвращения взрыва кардинальности в больших сетях. Набор меток метрики `netmon_tcp_loss_total` зависит от `cardinality.level`:

| Уровень | Метки на `netmon_tcp_loss_total` |
|---------|-----------------------------------|
| `role` (по умолчанию) | `src_location, dst_location, src_role, dst_role, src_vrf, dst_vrf` (без IP, без network) |
| `network` | `src_network, dst_network, src_location, dst_location, src_role, dst_role, src_vrf, dst_vrf` (/24, без IP) |
| `ip` | `src_ip, dst_ip, src_location, dst_location, src_role, dst_role, src_network, dst_network, src_vrf, dst_vrf` (без ограничений) |

**Для получения метрик на каждый IP (использовать с осторожностью):**
```yaml
metrics:
  cardinality:
    level: ip  # ⚠️ Создаёт один сериас на каждую уникальную пару IP
    max_series: 100000  # Увеличить лимит при необходимости
```

> ⚠️ **Предупреждение:** Установка `level: ip` в большой сети может создать тысячи серий и вызвать OOM как на netmon, так и на Prometheus. Используйте `role` или `network` для продакшена.

### Маппинг IP → роль/локация (Longest Prefix Match)

netmon использует **longest prefix match** для сопоставления IP → роль/локация. Более специфичные маршруты побеждают:

**Пример:**
```yaml
# roles.yaml
roles:
  - network: 10.179.64.0/22    # Общий диапазон датацентра
    role: datacenter
  - network: 10.179.64.32/32   # Конкретный хост побеждает для этого IP
    role: s3-dwh05
```

Для IP `10.179.64.32` роль будет `s3-dwh05` (а не `datacenter`).

**Смотрите примеры конфигов:**
- [`configs/roles.example.yaml`](../configs/roles.example.yaml)
- [`configs/locations.example.yaml`](../configs/locations.example.yaml)

### Аутентификация

**Установка токена через переменную окружения (рекомендуется):**
```bash
export NETMON_AUTH_TOKEN="ваш-секретный-токен"
sudo systemctl restart netmon
```

**Или в файле конфигурации:**
```yaml
global:
  auth_token: "ваш-секретный-токен"
```

**Защищённые эндпоинты:** `/metrics`, `/api/*`
**Публичные эндпоинты:** `/health`, `/ready`

**Пример curl с аутентификацией:**
```bash
curl -H "Authorization: Bearer ваш-секретный-токен" http://localhost:9876/metrics
```

---

## 3. systemd-сервис

### Пример unit-файла (`/etc/systemd/system/netmon.service`)

```ini
[Unit]
Description=Network Monitor - отслеживание потерь TCP-пакетов (eBPF)
Documentation=https://github.com/vponomarev/network-monitor/blob/main/docs/PRODUCTION_ru.md
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root

# Бинарный файл и конфигурация
ExecStart=/usr/local/bin/netmon --config /etc/netmon/config.yaml
ExecReload=/bin/kill -HUP $MAINPID

# Политика перезапуска (важно с обработкой фатальных ошибок TASK-09)
# При фатальной ошибке коллектора netmon завершается с кодом 1 → systemd перезапускает его
Restart=on-failure
RestartSec=5
StartLimitIntervalSec=60
StartLimitBurst=5

# Окружение
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
Environment=GOMAXPROCS=2
# Environment=NETMON_AUTH_TOKEN=ваш-секретный-токен

# Рабочая директория
WorkingDirectory=/var/lib/netmon

# Усиление безопасности
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true

# Capabilities. CAP_SYS_ADMIN покрывает загрузку eBPF + attach трейспоинта (perf)
# на всех проверенных ядрах; CAP_NET_RAW нужен только для traceroute (discovery).
AmbientCapabilities=CAP_SYS_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_SYS_ADMIN CAP_NET_RAW

# Least-privilege альтернатива (может не сделать attach трейспоинта на ядрах с
# perf_event_paranoid > 1 — см. раздел "Необходимые capabilities" выше):
# AmbientCapabilities=CAP_BPF CAP_PERFMON CAP_NET_RAW
# CapabilityBoundingSet=CAP_BPF CAP_PERFMON CAP_NET_RAW

# Файловые дескрипторы
LimitNOFILE=65536
LimitNPROC=4096

# Лимит памяти (опционально, настроить под кардинальность)
MemoryMax=512M

# Логирование
StandardOutput=journal
StandardError=journal
SyslogIdentifier=netmon

[Install]
WantedBy=multi-user.target
```

### Установка

```bash
# Скопировать файл сервиса
sudo cp packaging/netmon.service /etc/systemd/system/netmon.service

# Перезагрузить systemd
sudo systemctl daemon-reload

# Включить и запустить
sudo systemctl enable netmon
sudo systemctl start netmon

# Проверить
sudo systemctl status netmon
```

### Перезагрузка конфигурации

```bash
# Graceful reload (SIGHUP) — перезагружает конфиг без разрыва соединений
sudo systemctl reload netmon

# Полный перезапуск (при обновлении бинарного файла)
sudo systemctl restart netmon
```

### Лимиты ресурсов

| Ресурс | По умолчанию | Рекомендуется (продакшен) |
|--------|--------------|---------------------------|
| Память | Без ограничений | `MemoryMax=512M` (настроить под кардинальность) |
| CPU | Без ограничений | `CPUQuota=100%` (1 ядро) |
| Файловые дескрипторы | 65536 | 65536 |
| Задержка перезапуска | 5с | 5с (с ограничением burst) |

---

## 4. Health & Readiness

### Эндпоинты

| Эндпоинт | Метод | Ответ | Назначение |
|----------|-------|-------|------------|
| `/health` | GET | `200 OK` (всегда, если процесс жив) | **Liveness** probe |
| `/ready` | GET | `200 OK` (коллектор запущен) или `503` (не готов) | **Readiness** probe |

### Примеры ответов

**Health (всегда 200, когда HTTP-сервер работает):**
```json
{"status":"ok"}
```

**Ready (коллектор работает):**
```json
{"status":"ready"}
```

**Not Ready (коллектор не запущен или остановлен):**
```json
{"status":"not ready","reason":"loss collector not started"}
```

### Kubernetes Probes

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 9876
  initialDelaySeconds: 5
  periodSeconds: 10
  timeoutSeconds: 3
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /ready
    port: 9876
  initialDelaySeconds: 5
  periodSeconds: 5
  timeoutSeconds: 3
  failureThreshold: 3
```

### Интеграция с systemd

```ini
# В секции [Service]
# netmon сигнализирует о готовности через /ready
# Используйте ExecStartPost для ожидания готовности перед запуском сервиса

ExecStartPost=/bin/sh -c 'for i in $(seq 1 30); do curl -sf http://localhost:9876/ready && exit 0; sleep 1; done; exit 1'
```

---

## 5. Метрики и алерты

### Основная метрика: потери TCP

```promql
# Счётчик TCP-ретрансмитов (одно событие = один ретрансмиттированный сегмент)
# Набор меток ниже — для уровня кардинальности по умолчанию "role":
netmon_tcp_loss_total{
    src_location="datacenter-a",
    dst_location="datacenter-b",
    src_role="app-server",
    dst_role="database",
    src_vrf="unknown",
    dst_vrf="unknown"
}
```

> ⚠️ **Примечание:** После TASK-01 эта метрика корректно инкрементируется на +1 на ретрансмит (а не на количество пакетов).

### Метрики самонаблюдения коллектора (TASK-08)

```promql
# Статус коллектора: 1 = работает, 0 = остановлен/упал
netmon_loss_collector_up

# События, прочитанные из ядра (ring buffer или trace_pipe)
netmon_loss_events_read_total

# События, успешно распарсенные
netmon_loss_events_parsed_total

# События, не распарсенные (указывает на несоответствие формата или повреждение)
netmon_loss_parse_errors_total

# Информация об источнике потерь (ebpf или tracepipe)
netmon_loss_source_info{source="ebpf"}  # 1 если активен
```

### Метрики кардинальности (TASK-10)

```promql
# Текущее количество активных серий (перед экспортом в Prometheus)
netmon_loss_active_series

# Кумулятивное количество серий, отброшенных из-за лимита max_series
netmon_loss_series_dropped_total
```

### PromQL-правила для алертов

```yaml
groups:
  - name: netmon
    rules:
      # Алерт 1: Коллектор упал
      - alert: NetmonCollectorDown
        expr: netmon_loss_collector_up == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Коллектор netmon остановлен"
          description: "netmon на {{ $labels.instance }} прекратил сбор данных"

      # Алерт 2: Ошибки парсинга (несоответствие формата)
      - alert: NetmonParseErrors
        expr: rate(netmon_loss_parse_errors_total[5m]) > 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Обнаружены ошибки парсинга netmon"
          description: "netmon не может распарсить события потерь на {{ $labels.instance }}"

      # Алерт 2b: События читаются, но ни одно не парсится (дрейф формата события)
      - alert: NetmonReadNotParsed
        expr: rate(netmon_loss_events_read_total[5m]) > 0 and rate(netmon_loss_events_parsed_total[5m]) == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "netmon читает события потерь, но не парсит ни одного"
          description: "Вероятно дрейф структуры eBPF/Go на {{ $labels.instance }} — проверьте версию ядра/сборки"

      # Алерт 3: Достигнут лимит кардинальности
      - alert: NetmonSeriesDropped
        expr: increase(netmon_loss_series_dropped_total[1h]) > 0
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "netmon отбрасывает серии из-за лимита кардинальности"
          description: "Увеличьте metrics.cardinality.max_series на {{ $labels.instance }}"

      # Алерт 4: Высокая частота потерь (пример, настроить пороги)
      - alert: NetmonHighLossRate
        expr: rate(netmon_tcp_loss_total[5m]) > 100  # Настроить порог
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Обнаружена высокая частота потерь TCP"
          description: "Путь {{ $labels.src_role }} → {{ $labels.dst_role }} имеет {{ $value }} ретрансмитов/сек"
```

### Grafana-дашборд

Смотрите [`dashboards/`](../dashboards/) для готового JSON дашборда Grafana.

---

## 6. Ограничения

### Только IPv4

**Текущая версия поддерживает только IPv4.** IPv6 не реализован в этом релизе.

- eBPF-программа: `bpf/tcploss.bpf.c` обрабатывает только IPv4
- Сопоставление метаданных: только IPv4 CIDR

### Ретрансмит как прокси-метрика потерь

**Этот инструмент измеряет TCP-ретрансмиты, а не абсолютные потери пакетов.**

Ретрансмит указывает, что пакет был потерян **где-то на пути**, но:
- Один ретрансмит ≠ один потерянный пакет (может быть ложным ретрансмитом)
- Несколько ретрансмитов на оригинальный пакет считаются отдельно
- Ретрансмиты могут происходить из-за перегрузки, а не только потерь

**Используйте эту метрику как относительный индикатор, а не абсолютный процент потерь.**

### Лимиты кардинальности

**По умолчанию `max_series: 10000` предотвращает неограниченный рост.**

Если видите `netmon_loss_series_dropped_total > 0`:
1. Проверьте `netmon_loss_active_series` — сколько активных серий?
2. Если легитимный трафик: увеличьте `max_series` в конфиге
3. Если неожиданно: исследуйте паттерны трафика, рассмотрите `level: role` вместо `ip`

### Совместимость с ядром

| Версия ядра | Статус | Примечания |
|-------------|--------|------------|
| **6.12** | ✅ Протестировано | Debian 13 |
| **6.8** | ✅ Протестировано | Proxmox 8.4 (база Debian 12) |
| **6.1** | ✅ Протестировано | Debian 12 |
| **5.15** | ✅ Протестировано | Ubuntu 22.04 |
| **5.8–5.14** | ⚙️ Поддерживается (здесь не тестировалось) | Ring buffer доступен; CO-RE требует наличия BTF |
| **<5.8** | ❌ eBPF-путь недоступен | Нет `BPF_MAP_TYPE_RINGBUF` — используйте `loss_source: tracepipe` |
| **<4.9** | ❌ Не поддерживается | Нет поддержки eBPF |

---

## Чек-лист быстрого старта

```bash
# 1. Проверить BTF ядра
ls /sys/kernel/btf/vmlinux

# 2. Установить бинарный файл
wget https://github.com/vponomarev/network-monitor/releases/latest/download/netmon-linux-amd64
sudo cp netmon-linux-amd64 /usr/local/bin/netmon
sudo chmod +x /usr/local/bin/netmon

# 3. Создать директории
sudo mkdir -p /etc/netmon /var/lib/netmon /var/log/netmon

# 4. Скопировать конфигурацию
sudo cp configs/config.example.yaml /etc/netmon/config.yaml
# Отредактировать /etc/netmon/config.yaml по необходимости

# 5. Скопировать роли/локации
sudo cp configs/roles.example.yaml /etc/netmon/roles.yaml
sudo cp configs/locations.example.yaml /etc/netmon/locations.yaml

# 6. Установить systemd-сервис
sudo cp packaging/netmon.service /etc/systemd/system/netmon.service
sudo systemctl daemon-reload
sudo systemctl enable netmon
sudo systemctl start netmon

# 7. Проверить
sudo systemctl status netmon
curl http://localhost:9876/health
curl http://localhost:9876/ready
curl http://localhost:9876/metrics | grep netmon_tcp_loss
```

---

## Решение проблем

### Коллектор не запускается

```bash
# Проверить логи
sudo journalctl -u netmon -n 50

# Частые ошибки:
# - "BTF not found" → Установить заголовки ядра или включить BTF
# - "permission denied" → Проверить capabilities в systemd unit
# - "config invalid" → Проверить синтаксис YAML
```

### Метрики не экспортируются

```bash
# Проверить, включён ли траспоинт
cat /sys/kernel/tracing/events/tcp/tcp_retransmit_skb/enable
# Должно выводить: 1

# Включить при необходимости
echo 1 | sudo tee /sys/kernel/tracing/events/tcp/tcp_retransmit_skb/enable

# Сгенерировать тестовый трафик
curl https://example.com

# Проверить метрики снова
curl http://localhost:9876/metrics | grep netmon_tcp_loss
```

### Высокое использование памяти

```bash
# Проверить активные серии
curl http://localhost:9876/metrics | grep netmon_loss_active_series

# Уменьшить кардинальность
# Отредактировать /etc/netmon/config.yaml:
# metrics:
#   cardinality:
#     level: role  # или "network"
#     max_series: 5000

sudo systemctl reload netmon
```

---

## См. также

- [Руководство по конфигурации](configuration.md) — Полный справочник конфигов
- [Руководство по eBPF-разработке](ebpf-guide.md) — Детали eBPF-программ
- [Развёртывание через systemd](SYSTEMD_DEPLOYMENT.md) — Legacy-руководство (trace_pipe)
- [Развёртывание в Docker](DOCKER_DEPLOYMENT.md) — Контейнерное развёртывание

---

*Последнее обновление: Июль 2026 | netmon v1.0+ (eBPF)*
