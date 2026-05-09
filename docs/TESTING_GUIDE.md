# Руководство по тестированию Network Monitor

Это руководство описывает все доступные тесты и способы их запуска на локальных и удалённых машинах.

---

## 📋 Содержание

- [Обзор тестов](#обзор-тестов)
- [Запуск локальных тестов](#запуск-локальных-тестов)
- [Запуск integration тестов](#запуск-integration-тестов)
- [Запуск на удалённой машине](#запуск-на-удалённой-машине)
- [Отчёт о покрытии](#отчёт-о-покрытии)
- [Устранение проблем](#устранение-проблем)

---

## 📊 Обзор тестов

### Unit тесты

| Модуль | Файлы тестов | Покрытие | Статус |
|--------|--------------|----------|--------|
| `internal/packetloss` | `packetloss_linux_test.go`, `packetloss_extended_test.go` | ~85% | ✅ Новые |
| `internal/bandwidth` | `bandwidth_test.go`, `bandwidth_extended_test.go` | ~75% | ✅ Новые |
| `internal/collector` | `trace_pipe_test.go`, `trace_pipe_integration_test.go` | ~90% | ✅ |
| `internal/discovery` | `cache_test.go`, `path_test.go`, `api_test.go` | ~92% | ✅ |
| `internal/metadata` | `location_test.go`, `role_test.go` | ~98% | ✅ |
| `internal/dns` | `dns_test.go` | ~94% | ✅ |
| `internal/latency` | `latency_test.go` | ~89% | ✅ |
| `internal/config` | `config_test.go` | ~38% | ⚠️ |
| `internal/conntrack` | `state_machine_test.go`, `syslog_test.go` | ~43% | ⚠️ |

### Integration тесты

| Тест | Файл | Требует root | Описание |
|------|------|--------------|----------|
| `TestTracePipeCollector_Integration` | `trace_pipe_integration_test.go` | ✅ | Работа с реальным trace_pipe |
| `TestTracePipeCollector_Integration_RealTraffic` | `trace_pipe_integration_extended_test.go` | ✅ | Трафик в реальном времени |
| `TestTracePipeCollector_Integration_HighLoad` | `trace_pipe_integration_extended_test.go` | ✅ | Высокая нагрузка |
| `TestTracePipeCollector_Integration_LongRunning` | `trace_pipe_integration_extended_test.go` | ✅ | Длительный запуск |
| `TestBandwidth_Integration` | `tests/integration/monitoring_test.go` | ✅ | Реальные интерфейсы |
| `TestLatency_Integration` | `tests/integration/monitoring_test.go` | ✅ | Реальные цели |
| `TestDNS_Integration` | `tests/integration/monitoring_test.go` | ✅ | DNS запросы |

### E2E тесты

| Тест | Файл | Описание |
|------|------|----------|
| `TestE2E_FullSystem` | `tests/e2e/system_test.go` | Полный запуск системы |
| `TestE2E_CliHelp` | `tests/e2e/system_test.go` | Проверка CLI |
| `TestE2E_Version` | `tests/e2e/system_test.go` | Проверка версии |

---

## 🚀 Запуск локальных тестов

### Все unit тесты (macOS/Linux)

```bash
cd /path/to/network-monitor

# Все тесты
go test -v ./internal/... ./cmd/... ./pkg/...

# С покрытием
go test -cover ./internal/...

# С race detector
go test -race ./internal/...
```

### Тесты конкретных модулей

```bash
# Packet loss (новые тесты)
go test -v ./internal/packetloss/...

# Bandwidth (новые тесты)
go test -v ./internal/bandwidth/...

# Collector
go test -v ./internal/collector/...

# Discovery
go test -v ./internal/discovery/...
```

### Только новые расширенные тесты

```bash
# Packet loss extended
go test -v -run "TestMonitor_" ./internal/packetloss/...

# Bandwidth extended
go test -v -run "TestMonitor_" ./internal/bandwidth/...

# Collector integration extended
go test -v -tags=integration -run "TestTracePipeCollector_Integration_" ./internal/collector/...
```

---

## 🔧 Запуск integration тестов

### Требования

- **Linux** (требуется доступ к /proc, trace_pipe)
- **Root права** (для доступа к trace_pipe и eBPF)
- **Смонтированный tracefs**: `mount -t tracefs none /sys/kernel/tracing`

### Запуск

```bash
# Все integration тесты
sudo go test -v -tags=integration ./internal/... ./tests/integration/...

# Только collector integration
sudo go test -v -tags=integration ./internal/collector/...

# С покрытием
sudo go test -v -tags=integration -coverprofile=integration_coverage.out ./internal/collector/...
```

### Отдельные integration тесты

```bash
# Реальный трафик
sudo go test -v -tags=integration -run TestTracePipeCollector_Integration_RealTraffic ./internal/collector/...

# Высокая нагрузка
sudo go test -v -tags=integration -run TestTracePipeCollector_Integration_HighLoad ./internal/collector/...

# Длительный запуск (10 секунд)
sudo go test -v -tags=integration -run TestTracePipeCollector_Integration_LongRunning ./internal/collector/...

# Несколько коллекторов
sudo go test -v -tags=integration -run TestTracePipeCollector_Integration_MultipleCollectors ./internal/collector/...

# Использование памяти
sudo go test -v -tags=integration -run TestTracePipeCollector_Integration_MemoryUsage ./internal/collector/...
```

---

## 🌐 Запуск на удалённой машине

### Скрипт run-remote-tests.sh

Автоматизирует запуск тестов на удалённых Linux хостах.

#### Настройка

```bash
# Переменные окружения (опционально)
export REMOTE_USER=root           # Пользователь SSH
export RUN_UNIT_TESTS=true        # Запуск unit тестов
export RUN_INTEGRATION_TESTS=true # Запуск integration тестов
export RUN_E2E_TESTS=false        # Запуск E2E тестов
export GENERATE_COVERAGE=true     # Генерация отчёта о покрытии
export CLEANUP=true               # Очистка после тестов
```

#### Использование

```bash
# Хосты по умолчанию (192.168.5.214, 192.168.5.193, 192.168.5.217, 192.168.5.99)
./scripts/run-remote-tests.sh

# Один хост
./scripts/run-remote-tests.sh 192.168.5.214

# Несколько хостов
./scripts/run-remote-tests.sh 192.168.5.214 192.168.5.193

# С явным пользователем
REMOTE_USER=admin ./scripts/run-remote-tests.sh 192.168.5.214
```

#### Что делает скрипт

1. ✅ Проверяет доступность хоста
2. ✅ Получает информацию о системе (OS, kernel, Go)
3. ✅ Устанавливает Go (если не установлен)
4. ✅ Копирует исходный код через rsync
5. ✅ Запускает unit тесты
6. ✅ Запускает integration тесты (требует root)
7. ✅ Генерирует отчёт о покрытии
8. ✅ Копирует отчёты локально
9. ✅ Очищает временные файлы (опционально)

#### Отчёты

После запуска скрипта в `./scripts/` появятся файлы:

```
scripts/
├── test_report_192.168.5.214_20260506_120000.summary.txt
├── test_report_192.168.5.214_20260506_120000.coverage.out
├── test_report_192.168.5.193_20260506_120100.summary.txt
└── test_report_192.168.5.193_20260506_120100.coverage.out
```

### Ручной запуск на удалённой машине

#### Шаг 1: Копирование файлов

```bash
# Копирование исходного кода
rsync -avz -e ssh /path/to/network-monitor/ root@192.168.5.214:/tmp/network-monitor-tests/

# Или через scp
scp -r /path/to/network-monitor/* root@192.168.5.214:/tmp/network-monitor-tests/
```

#### Шаг 2: Подключение и запуск

```bash
# Подключение к хосту
ssh root@192.168.5.214

# Переход в директорию тестов
cd /tmp/network-monitor-tests

# Загрузка зависимостей
export PATH=/usr/local/go/bin:$PATH
go mod download

# Unit тесты
go test -v ./internal/packetloss/...
go test -v ./internal/bandwidth/...
go test -v ./internal/collector/...

# Integration тесты
sudo go test -v -tags=integration ./internal/collector/...
sudo go test -v -tags=integration ./tests/integration/...

# Покрытие
go test -coverprofile=coverage.out ./internal/...
go tool cover -func=coverage.out | grep -E '(packetloss|bandwidth|collector):'
```

#### Шаг 3: Копирование результатов

```bash
# Копирование отчёта о покрытии
scp root@192.168.5.214:/tmp/network-monitor-tests/coverage.out ./coverage_remote.out

# HTML отчёт (если нужен)
scp root@192.168.5.214:/tmp/network-monitor-tests/coverage.html ./coverage_remote.html
```

---

## 📈 Отчёт о покрытии

### Генерация

```bash
# Общее покрытие
go test -coverprofile=coverage.out ./internal/...

# Просмотр в терминале
go tool cover -func=coverage.out

# HTML отчёт
go tool cover -html=coverage.out -o coverage.html

# Покрытие конкретных модулей
go tool cover -func=coverage.out | grep packetloss
go tool cover -func=coverage.out | grep bandwidth
go tool cover -func=coverage.out | grep collector
```

### Целевые показатели

| Модуль | Текущее | Цель | Статус |
|--------|---------|------|--------|
| `packetloss` | ~85% | 80% | ✅ Достигнуто |
| `bandwidth` | ~75% | 70% | ✅ Достигнуто |
| `collector` | ~90% | 85% | ✅ Достигнуто |
| `discovery` | ~92% | 85% | ✅ Достигнуто |
| `config` | ~38% | 60% | ⚠️ Требуется работа |
| `conntrack` | ~43% | 60% | ⚠️ Требуется работа |

---

## 🔍 Устранение проблем

### Тесты не запускаются на macOS

**Проблема:** `build constraints exclude all Go files`

**Решение:** Это нормально для Linux-specific тестов. Используйте:

```bash
# Только кроссплатформенные тесты
go test -v ./internal/config/... ./internal/metadata/...

# Или используйте Docker/Linux VM
docker run --rm -v $(pwd):/app -w /app golang:1.24 go test -v ./internal/...
```

### Integration тесты требуют root

**Проблема:** `permission denied` или `operation not permitted`

**Решение:**

```bash
# Запуск от root
sudo go test -v -tags=integration ./internal/...

# Или с capabilities (не рекомендуется для production)
sudo setcap cap_sys_admin+ep $(which go)
```

### trace_pipe недоступен

**Проблема:** `trace_pipe not found`

**Решение:**

```bash
# Смонтировать tracefs
sudo mount -t tracefs none /sys/kernel/tracing

# Проверить
ls -la /sys/kernel/tracing/trace_pipe
```

### Тесты зависают

**Проблема:** Тесты не завершаются в течение 5+ минут

**Решение:**

```bash
# Увеличить таймаут
go test -v -timeout=10m ./internal/...

# Запустить с отладкой
go test -v -run TestTracePipeCollector_Integration_LongRunning ./internal/collector/... 2>&1 | tee debug.log

# Прервать и очистить
pkill -9 go
rm -rf /tmp/network-monitor-tests
```

### Недостаточно памяти на удалённой машине

**Проблема:** `runtime: out of memory`

**Решение:**

```bash
# Ограничить использование памяти
GOMEMLIMIT=512MiB go test -v ./internal/...

# Запускать тесты по отдельности
go test -v ./internal/packetloss/...
go test -v ./internal/bandwidth/...
go test -v ./internal/collector/...
```

---

## 📚 Дополнительные ресурсы

- [CONTRIBUTING.md](../CONTRIBUTING.md) — Руководство по внесению изменений
- [TEST_COVERAGE.md](TEST_COVERAGE.md) — Детальный отчёт о покрытии
- [README_CONNTRACK_TESTS.md](README_CONNTRACK_TESTS.md) — Тесты conntrack

---

*Последнее обновление: 2026-05-06*
