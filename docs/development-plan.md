# План развития: Анализ потерь TCP пакетов (v4.0 - Implementation Plan)

## Решения по архитектуре

| Вопрос | Решение | Обоснование |
|--------|---------|-------------|
| 1. Объём реализации | **Полный функционал, но MVP-first** | Можно использовать сразу, расширяя постепенно |
| 2. Формат конфигурации | **YAML только** | Проще поддержка, нет legacy |
| 3. Совместимость | **Не требуется** | Чистый дизайн без оглядки на Python |
| 4. Discovery | **Top-N + On-demand API** | Максимальная гибкость |
| 5. Визуализация | **Grafana JSON в репозитории** | Работает "из коробки" |

---

## 1. Минимальный жизнеспособный продукт (MVP)

### 1.1. Что входит в MVP

```
┌─────────────────────────────────────────────────────────────┐
│  MVP Scope - Можно использовать сразу                       │
├─────────────────────────────────────────────────────────────┤
│  ✅ Сбор данных из trace_pipe                               │
│  ✅ YAML конфигурация (locations, roles)                    │
│  ✅ Best-match маппинг IP → Location/Role                   │
│  ✅ Prometheus метрика netmon_tcp_loss_total                │
│  ✅ HTTP /metrics endpoint                                  │
│  ✅ SIGHUP для reload конфигурации                          │
│  ✅ Базовый Grafana дашборд                                 │
├─────────────────────────────────────────────────────────────┤
│  🔄 Discovery (Top-N + On-demand) - Phase 2                 │
│  🔄 Топология (Leaf/Spine) - Phase 3                        │
│  🔄 CMDB интеграция - Future                                │
└─────────────────────────────────────────────────────────────┘
```

### 1.2. Структура MVP

```
cmd/netmon/
├── main.go                 # Точка входа
├── collector/
│   ├── trace_pipe.go       # Чтение trace_pipe
│   └── retransmit.go       # Счётчик ретрансмитов
├── metadata/
│   ├── matcher.go          # Best-match логика
│   ├── location.go         # Location маппинг
│   └── role.go             # Role маппинг
├── metrics/
│   ├── exporter.go         # Prometheus экспорт
│   └── server.go           # HTTP сервер
└── config/
    └── config.go           # YAML загрузка
```

---

## 2. Конфигурация (YAML Only)

### 2.1. Основной конфиг

```yaml
# config.yaml

global:
  ttl_hours: 3
  metrics_port: 9876
  trace_pipe_path: /sys/kernel/tracing/trace_pipe

metadata:
  locations:
    type: file
    path: locations.yaml
    
  roles:
    type: file
    path: roles.yaml

discovery:
  traceroute:
    enabled: true
    top_n: 10
    mode: both  # both | top_loss | on_demand | periodic
    interval: 5m

metrics:
  name: netmon_tcp_loss_total
  default_labels:
    - src_ip
    - dst_ip
    - src_location
    - dst_location
    - src_role
    - dst_role
  optional_labels:
    - src_network
    - dst_network
    - path_id

logging:
  level: info
  format: json
```

### 2.2. Locations

```yaml
# locations.yaml
locations:
  - network: 10.146.22.0/24
    location: IX-M4-SM3
    
  - network: 10.179.64.0/22
    location: IX-M5-SM13
    
  - network: 10.179.65.31/32
    location: IX-M5-SM13
    hostname: dwh-lb-01
```

### 2.3. Roles

```yaml
# roles.yaml
roles:
  - network: 10.179.64.32/32
    role: s3-dwh05
    
  - network: 10.179.65.31/32
    role: dwh-lb
    
  - network: 10.179.64.0/22
    role: dwh-storage
```

---

## 3. Discovery (Top-N + On-demand)

### 3.1. Архитектура

```
┌──────────────────────────────────────────────────────────────┐
│                      Discovery Engine                         │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌─────────────────┐         ┌─────────────────┐            │
│  │   Top-N Mode    │         │  On-demand Mode │            │
│  │                 │         │                 │            │
│  │ 1. Get top 10   │         │ 1. HTTP POST    │            │
│  │    loss pairs   │         │    /api/discover│            │
│  │                 │         │                 │            │
│  │ 2. Run traceroute│        │ 2. Run traceroute│           │
│  │    for each     │         │    for pair     │            │
│  │                 │         │                 │            │
│  │ 3. Update every │         │ 3. Return result│            │
│  │    5 minutes    │         │    immediately  │            │
│  └─────────────────┘         └─────────────────┘            │
│                                                              │
│                    ↓                                         │
│         ┌─────────────────────┐                              │
│         │   Path Cache        │                              │
│         │   (TTL: 10 min)     │                              │
│         └─────────────────────┘                              │
│                                                              │
│                    ↓                                         │
│         ┌─────────────────────┐                              │
│         │   Metrics Enrich    │                              │
│         │   (add path_id)     │                              │
│         └─────────────────────┘                              │
└──────────────────────────────────────────────────────────────┘
```

### 3.2. API

```bash
# On-demand discovery
curl -X POST http://localhost:9876/api/v1/discover \
  -H "Content-Type: application/json" \
  -d '{"src_ip": "10.179.64.32", "dst_ip": "10.181.208.50"}'

# Ответ
{
  "path_id": "path-001",
  "hops": [
    {"ttl": 1, "ip": "10.179.64.1", "hostname": "leaf-01", "loss": 0},
    {"ttl": 2, "ip": "10.0.0.1", "hostname": "spine-01", "loss": 2},
    {"ttl": 3, "ip": "10.181.208.1", "hostname": "leaf-03", "loss": 0}
  ],
  "bottleneck": {
    "hop_ip": "10.0.0.1",
    "loss_percent": 2.5
  }
}
```

---

## 4. Метрики

### 4.1. Основная метрика

```prometheus
# HELP netmon_tcp_loss_total Total number of TCP retransmissions
# TYPE netmon_tcp_loss_total counter
netmon_tcp_loss_total{
    src_ip="10.179.64.32",
    dst_ip="10.181.208.50",
    src_location="IX-M5-SM13",
    dst_location="IX-M3-SM10",
    src_role="s3-dwh05",
    dst_role="dwh-lb",
    src_network="10.179.64.0/22",
    dst_network="10.181.208.0/22",
    path_id="path-001"
} 15
```

### 4.2. Служебные метрики

```prometheus
# HELP netmon_discovery_paths_total Total number of discovered paths
# TYPE netmon_discovery_paths_total gauge
netmon_discovery_paths_total 10

# HELP netmon_discovery_last_run_seconds Timestamp of last discovery run
# TYPE netmon_discovery_last_run_seconds gauge
netmon_discovery_last_run_seconds 1714060800

# HELP netmon_config_reload_total Total number of config reloads
# TYPE netmon_config_reload_total counter
netmon_config_reload_total 3

# HELP netmon_trace_pipe_errors_total Total errors reading trace_pipe
# TYPE netmon_trace_pipe_errors_total counter
netmon_trace_pipe_errors_total 0
```

---

## 5. Grafana Дашборд

### 5.1. Структура

Файл: `dashboards/tcp-loss-analysis.json`

```
Dashboard: TCP Loss Analysis
│
├── Row 1: Global Overview
│   ├── Panel: Total Retransmits (Stat)
│   ├── Panel: Top Location Pairs (Table)
│   └── Panel: Top Role Pairs (Table)
│
├── Row 2: Network View
│   ├── Panel: Loss by Location Pair (Bar chart)
│   ├── Panel: Loss by Role Pair (Bar chart)
│   └── Panel: Top 10 IP Pairs (Table)
│
├── Row 3: Timeline
│   └── Panel: Retransmits Over Time (Time series)
│
└── Row 4: Discovery (Phase 2)
    ├── Panel: Discovered Paths (Table)
    └── Panel: Bottleneck by Hop (Heatmap)
```

### 5.2. Переменные

```json
{
  "templating": {
    "list": [
      {
        "name": "src_location",
        "label": "Source Location",
        "type": "query",
        "query": "label_values(netmon_tcp_loss_total, src_location)"
      },
      {
        "name": "dst_location", 
        "label": "Destination Location",
        "type": "query",
        "query": "label_values(netmon_tcp_loss_total{src_location=\"$src_location\"}, dst_location)"
      },
      {
        "name": "src_role",
        "label": "Source Role",
        "type": "query",
        "query": "label_values(netmon_tcp_loss_total, src_role)"
      },
      {
        "name": "time_range",
        "label": "Time Range",
        "type": "custom",
        "options": ["5m", "15m", "1h", "6h", "24h"]
      }
    ]
  }
}
```

---

## 6. План реализации (Phased)

### Phase 1: MVP Core (1-2 недели)

| Задача | Файл | Статус |
|--------|------|--------|
| 1.1 Парсинг trace_pipe | `collector/trace_pipe.go` | ⬜ |
| 1.2 YAML конфиг loader | `config/config.go` | ⬜ |
| 1.3 Best-match matcher | `metadata/matcher.go` | ⬜ |
| 1.4 Prometheus exporter | `metrics/exporter.go` | ⬜ |
| 1.5 HTTP server | `metrics/server.go` | ⬜ |
| 1.6 SIGHUP handler | `main.go` | ⬜ |
| 1.7 Базовый Grafana dashboard | `dashboards/` | ⬜ |

**Результат**: Работающий экспортер с метриками.

### Phase 2: Discovery (1-2 недели)

| Задача | Файл | Статус |
|--------|------|--------|
| 2.1 Traceroute реализация | `discovery/traceroute.go` | ⬜ |
| 2.2 Top-N selector | `discovery/top_loss.go` | ⬜ |
| 2.3 HTTP API handler | `api/discover.go` | ⬜ |
| 2.4 Path cache | `discovery/cache.go` | ⬜ |
| 2.5 Metrics enrichment | `metrics/enrich.go` | ⬜ |

**Результат**: Авто-обнаружение путей + API.

### Phase 3: Topology (1-2 недели)

| Задача | Файл | Статус |
|--------|------|--------|
| 3.1 Topology model | `topology/model.go` | ⬜ |
| 3.2 Leaf/Spine support | `topology/leaf_spine.go` | ⬜ |
| 3.3 Network labels | `metrics/topology_labels.go` | ⬜ |
| 3.4 Topology dashboard | `dashboards/topology.json` | ⬜ |

**Результат**: Поддержка сетевой топологии.

### Phase 4: Polish (1 неделя)

| Задача | Файл | Статус |
|--------|------|--------|
| 4.1 Documentation | `docs/` | ⬜ |
| 4.2 Example configs | `examples/` | ⬜ |
| 4.3 Docker image | `Dockerfile` | ⬜ |
| 4.4 Release automation | `.github/workflows/` | ⬜ |

**Результат**: Готово к продакшену.

---

## 7. Структура проекта (Final)

```
network-monitor/
├── cmd/
│   └── netmon/
│       └── main.go
├── internal/
│   ├── collector/
│   │   ├── trace_pipe.go
│   │   └── trace_pipe_test.go
│   ├── metadata/
│   │   ├── matcher.go
│   │   ├── matcher_test.go
│   │   ├── location.go
│   │   └── role.go
│   ├── discovery/
│   │   ├── traceroute.go
│   │   ├── top_loss.go
│   │   ├── cache.go
│   │   └── api.go
│   ├── metrics/
│   │   ├── exporter.go
│   │   ├── server.go
│   │   └── enrich.go
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   └── topology/
│       ├── model.go
│       └── leaf_spine.go
├── configs/
│   ├── config.example.yaml
│   ├── locations.example.yaml
│   └── roles.example.yaml
├── dashboards/
│   └── tcp-loss-analysis.json
├── docs/
│   ├── README.md
│   ├── development-plan.md
│   ├── configuration.md
│   └── grafana-setup.md
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
└── README.md
```

---

## 8. Следующие шаги

1. **Создать структуру проекта**
2. **Реализовать Phase 1 (MVP Core)**
3. **Протестировать на пилотных серверах**
4. **Добавить Phase 2 (Discovery)**
5. **Добавить Phase 3 (Topology)**

---

*Документ версии 4.0 - Implementation Plan*
