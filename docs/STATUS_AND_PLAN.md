# Network Monitor — Статус и План Разработки

*Документ обновлён: 2026-04-27*

---

## 📊 ОБЗОР ПРОЕКТА

Сетевой монитор состоит из **двух независимых приложений**:

| Приложение | Каталог | Назначение | Готовность |
|------------|---------|------------|------------|
| **netmon** | `cmd/netmon` | Мониторинг TCP-потерь через trace_pipe | 100% |
| **conntrack** | `cmd/conntrack` | Трекинг соединений через eBPF | 95% |

---

## ✅ ВЫПОЛНЕНО В PHASE 1

### conntrack
- [x] #1 Создан Makefile для сборки eBPF (`bpf/Makefile`)
- [x] #2 Интегрирована загрузка eBPF из ELF файла (`tracker_linux.go`)
- [x] #3 Создан `vmlinux.h` с необходимыми определениями ядра
- [x] #4 Конфигурация перемещена в `types.go` (общий файл)
- [x] #5 Добавлен `DefaultEBPFProgramPath` для пути по умолчанию
- [x] #6 Обновлён `cmd/conntrack/main.go` для использования пути по умолчанию

### netmon
- [x] #1 Trace pipe collector готов к работе
- [x] #2 Topology полностью интегрирован в main.go
- [x] #3 Созданы integration tests (`tests/integration/core_test.go`)

---

## ✅ ВЫПОЛНЕНО В PHASE 2

### netmon
- [x] #4 Dashboard update — добавлены панели Discovery & Path Analysis
- [x] #5 API documentation — создан `docs/DISCOVERY_API.md`
- [x] #6 Config validation — расширена валидация всех секций конфига

### conntrack
- [x] #4 Byte count tracking — добавлены метрики `conntrack_bytes_total` и `conntrack_bytes_per_connection`
- [x] #5 Connection duration metrics — добавлены гистограммы длительности соединений
- [x] #6 Process name resolution — улучшена очистка имён процессов из eBPF

---

## ✅ ВЫПОЛНЕНО В PHASE 3

### Infrastructure
- [x] #1 Docker multi-stage build — поддержка netmon, conntrack и combined образов
- [x] #2 Docker Compose — обновлён для обоих приложений + Prometheus/Grafana профили
- [x] #3 GitHub Actions release workflow — сборка обоих приложений, eBPF, Docker
- [x] #4 README — полная документация с архитектурой, API, метриками

---

## 📊 МЕТРИКИ ГОТОВНОСТИ

| Приложение | Код | Тесты | Документация | Сборка | Итого |
|------------|-----|-------|--------------|--------|-------|
| netmon | 100% | 95% | 100% | 100% | 100% |
| conntrack | 95% | 90% | 95% | 95% | 95% |

---

## 📝 НОВЫЕ МЕТРИКИ CONNTRACK (Phase 2)

### Byte Tracking
```prometheus
# Total bytes transferred by direction and type
conntrack_bytes_total{direction="outgoing", type="sent"}
conntrack_bytes_total{direction="outgoing", type="received"}
conntrack_bytes_total{direction="incoming", type="sent"}
conntrack_bytes_total{direction="incoming", type="received"}

# Bytes per connection histogram
conntrack_bytes_per_connection{direction="outgoing"}
conntrack_bytes_per_connection{direction="incoming"}
```

### Connection Duration
```prometheus
# TCP handshake duration histogram
conntrack_handshake_duration_seconds{direction="outgoing"}
conntrack_handshake_duration_seconds{direction="incoming"}

# Total connection duration histogram
conntrack_connection_duration_seconds{direction="outgoing"}
conntrack_connection_duration_seconds{direction="incoming"}
```

---

## 📋 СЛЕДУЮЩИЕ ШАГИ (Phase 4 — Polish)

### Опциональные улучшения
- [ ] WebSocket API для real-time событий conntrack
- [ ] JSON file export с ротацией для conntrack
- [ ] Kubernetes manifests (DaemonSet + ConfigMap)
- [ ] Helm chart для развёртывания
- [ ] Alert rules для Prometheus
- [ ] Performance benchmarks

### Документация
- [ ] Production deployment guide
- [ ] Troubleshooting guide
- [ ] Performance tuning guide

---

*Документ будет обновляться по мере выполнения задач*
