# MVP Status Report

## ✅ Реализовано (Phase 1 - MVP Core)

### Основные компоненты

| Компонент | Файл | Статус |
|-----------|------|--------|
| **Main application** | `cmd/netmon/main.go` | ✅ |
| **Config loader (YAML)** | `internal/config/config.go` | ✅ |
| **Location matcher** | `internal/metadata/location.go` | ✅ |
| **Role matcher** | `internal/metadata/role.go` | ✅ |
| **Trace pipe collector** | `internal/collector/trace_pipe.go` | ✅ |
| **Prometheus exporter** | `internal/metrics/exporter.go` | ✅ |
| **HTTP server** | `internal/metrics/server.go` | ✅ |
| **SIGHUP handler** | `cmd/netmon/main.go` | ✅ |

### Тесты

| Тест | Файл | Статус |
|------|------|--------|
| Config tests | `internal/config/config_test.go` | ✅ |
| Metadata matcher tests | `internal/metadata/matcher_test.go` | ✅ |
| Collector tests | `internal/collector/trace_pipe_test.go` | ✅ |
| Exporter tests | `internal/metrics/exporter_test.go` | ✅ |

### Конфигурация

| Файл | Описание |
|------|----------|
| `configs/config.example.yaml` | Пример основного конфига |
| `configs/locations.example.yaml` | Пример locations (на основе твоих CSV) |
| `configs/roles.example.yaml` | Пример roles (на основе твоих CSV) |

### Визуализация

| Файл | Описание |
|------|----------|
| `dashboards/tcp-loss-analysis.json` | Grafana dashboard (готов к импорту) |

### Документация

| Файл | Описание |
|------|----------|
| `README.md` | Обновлён с инструкциями по запуску |
| `docs/development-plan.md` | Полный план развития (v4.0) |

---

## 🚀 Как использовать

### 1. Подготовка

```bash
# Собрать
make build

# Скопировать конфиги
cp configs/*.yaml .
```

### 2. Настроить

Отредактировать `locations.yaml` и `roles.yaml` со своими данными.

Формат аналогичен твоим CSV, но в YAML:

```yaml
# locations.yaml
locations:
  - network: 10.146.22.0/24
    location: IX-M4-SM3
    
  - network: 10.179.64.0/22
    location: IX-M5-SM13

# roles.yaml
roles:
  - network: 10.179.64.32/32
    role: s3-dwh05
    
  - network: 10.179.65.31/32
    role: dwh-lb
```

### 3. Запустить

```bash
# Смонтировать tracefs (если не смонтирован)
sudo mount -t tracefs none /sys/kernel/tracing

# Запустить
sudo ./bin/netmon
```

### 4. Проверить

```bash
# Metrics
curl http://localhost:9876/metrics

# Health
curl http://localhost:9876/health
```

### 5. Перезагрузить конфиг (без рестарта)

```bash
kill -HUP $(pidof netmon)
```

---

## 📊 Метрики

```prometheus
# Основная метрика
netmon_tcp_loss_total{
    src_ip="10.179.64.32",
    dst_ip="10.181.208.50",
    src_location="IX-M5-SM13",
    dst_location="IX-M3-SM10",
    src_role="s3-dwh05",
    dst_role="dwh-lb",
    src_network="10.179.64.0/22",
    dst_network="10.181.208.0/22"
} 15
```

---

## 📈 Grafana Dashboard

Готовый дашборд в `dashboards/tcp-loss-analysis.json`.

**Панели:**
1. Total Retransmits/sec (Stat)
2. Active Connection Pairs (Stat)
3. Retransmits by Location Pair (Time series)
4. Top 10 Location Pairs (Table)
5. Top 10 Role Pairs (Table)
6. Top 10 IP Pairs (Table)

**Переменные:**
- Source Location
- Destination Location
- Source Role

---

## 🔄 Что дальше (Phase 2+)

### Phase 2: Discovery (Traceroute)

- [ ] `internal/discovery/traceroute.go` - Traceroute реализация
- [ ] `internal/discovery/top_loss.go` - Выбор top-N по потерям
- [ ] `internal/discovery/api.go` - HTTP API для on-demand
- [ ] `internal/discovery/cache.go` - Кэш путей

### Phase 3: Topology

- [ ] `internal/topology/model.go` - Модель топологии
- [ ] `internal/topology/leaf_spine.go` - Leaf/Spine поддержка
- [ ] `internal/metrics/enrich.go` - enrichment метрик

### Phase 4: Polish

- [ ] Docker image
- [ ] Release automation
- [ ] Полная документация

---

## 📝 Заметки

### Best-Match логика

Как в Python версии — самый специфичный префикс побеждает:

```
IP: 10.179.64.32

Матчи:
1. 10.179.64.32/32 → s3-dwh05     ← WIN
2. 10.179.64.0/22  → DWH
3. 10.179.0.0/16   → DataCenter
```

### Отличия от Python версии

| Аспект | Python | Go (netmon) |
|--------|--------|-------------|
| Формат конфига | CSV | YAML |
| Метрика | `tcp_retransmits_total` | `netmon_tcp_loss_total` |
| Порт | 9876 | 9876 (same) |
| SIGHUP | ✅ | ✅ |
| Textfile collector | ✅ | ❌ (только HTTP) |

---

## ✅ Checklist для продакшена

- [ ] Смонтировать tracefs на всех серверах
- [ ] Создать конфиги с актуальными locations/roles
- [ ] Настроить Prometheus scrape
- [ ] Импортировать Grafana dashboard
- [ ] Настроить алертинг (опционально)

---

*MVP готов к использованию. Discovery и Topology — в следующей фазе.*
