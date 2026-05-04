# Тесты для проверки отслеживания подключений (Conntrack)

Этот документ описывает набор тестов для проверки работы приложения conntrack на разных версиях ядра Linux.

## 📋 Содержание

- [Обзор тестов](#обзор-тестов)
- [Требования](#требования)
- [Запуск тестов](#запуск-тестов)
- [Тесты для удаленных хостов](#тесты-для-удаленных-хостов)
- [Интерпретация результатов](#интерпретация-результатов)
- [Устранение неполадок](#устранение-неполадок)

---

## 📊 Обзор тестов

### Локальные интеграционные тесты

| Тест | Описание | Требует root |
|------|----------|--------------|
| `TestConntrack_OutgoingConnections` | Проверка отслеживания исходящих TCP подключений | ✅ |
| `TestConntrack_IncomingConnections` | Проверка отслеживания входящих TCP подключений | ✅ |
| `TestConntrack_TCPhandshake` | Полный TCP handshake (SYN → SYN+ACK → ESTABLISHED) | ✅ |
| `TestConntrack_ConnectionLifecycle` | Жизненный цикл подключения (NEW → ESTABLISHED → CLOSED) | ✅ |
| `TestConntrack_DirectionTracking` | Разделение на входящие/исходящие подключения | ✅ |
| `TestConntrack_ProcessIdentification` | Определение процесса (PID и имя) | ✅ |
| `TestConntrack_ConcurrentConnections` | Работа при конкурентных подключениях | ✅ |
| `TestConntrack_ConfigValidation` | Валидация конфигурации | ❌ |
| `TestConntrack_EventChannel` | Проверка канала событий | ✅ |
| `TestConntrack_MetricsIntegration` | Интеграция с метриками Prometheus | ✅ |
| `TestConntrack_AppConfig` | Загрузка конфигурации из файла | ❌ |

### Тесты состояния (state machine)

| Тест | Описание |
|------|----------|
| `TestConnectionState_String` | Проверка строковых представлений состояний |
| `TestConnectionEvent_String` | Проверка строковых представлений событий |
| `TestConnection_Duration` | Проверка длительности подключения |
| `TestConnection_HandshakeDuration` | Проверка длительности handshake |
| `TestConnection_Direction` | Проверка направления подключения |
| `StateMachine_*` | Тесты состояния TCP FSM |

---

## 🔧 Требования

### Системные требования

- **ОС**: Linux (kernel 4.9+)
- **Права**: root (для интеграционных тестов)
- **Go**: 1.21+
- **eBPF**: Поддержка eBPF (для тестов с реальным eBPF)

### Для удаленных тестов

- SSH доступ к целевым хостам
- Установленный Go на удаленных хостах
- rsync или scp для копирования файлов

---

## 🚀 Запуск тестов

### Локальные тесты

```bash
# Все интеграционные тесты (требует root)
sudo make test-integration

# Или напрямую
sudo go test -v ./tests/integration/...

# Только тесты conntrack
sudo go test -v ./tests/integration/... -run "TestConntrack"

# Тесты с покрытием
sudo go test -v -coverprofile=coverage.out ./tests/integration/...
go tool cover -html=coverage.out -o coverage.html
```

### Отдельные тесты

```bash
# Исходящие подключения
sudo go test -v ./tests/integration/... -run "TestConntrack_OutgoingConnections"

# Входящие подключения
sudo go test -v ./tests/integration/... -run "TestConntrack_IncomingConnections"

# TCP handshake
sudo go test -v ./tests/integration/... -run "TestConntrack_TCPhandshake"

# Конкурентные подключения
sudo go test -v ./tests/integration/... -run "TestConntrack_ConcurrentConnections"
```

---

## 🌐 Тесты для удаленных хостов

### Автоматический запуск

```bash
# Запуск на хостах по умолчанию (192.168.5.99 и 192.168.5.214)
./scripts/run-remote-tests.sh

# Запуск на конкретных хостах
./scripts/run-remote-tests.sh 192.168.5.99 192.168.5.214

# Запуск на одном хосте
./scripts/run-remote-tests.sh 192.168.5.99
```

### Ручной запуск

```bash
# 1. Копирование файлов на удаленный хост
scp -r tests/integration/ root@192.168.5.99:/tmp/network-monitor-tests/
scp go.mod go.sum root@192.168.5.99:/tmp/network-monitor-tests/

# 2. Подключение к хосту
ssh root@192.168.5.99

# 3. Запуск тестов
cd /tmp/network-monitor-tests/tests/integration
go mod download
sudo go test -v -run "TestConntrack" .
```

---

## 📈 Интерпретация результатов

### Успешный тест

```
=== RUN   TestConntrack_OutgoingConnections
    conntrack_connection_test.go:45: Tracked 3 connections
    conntrack_connection_test.go:52: Stats: {TotalOutgoing:3 TotalIncoming:0 ...}
--- PASS: TestConntrack_OutgoingConnections (2.15s)
```

### Проверка ядер

После запуска на удаленных хостах проверьте версию ядра:

```bash
# Проверка версии ядра
ssh root@192.168.5.99 "uname -r"
ssh root@192.168.5.214 "uname -r"

# Проверка поддержки eBPF
ssh root@192.168.5.99 "ls -la /sys/kernel/btf/vmlinux"
ssh root@192.168.5.214 "ls -la /sys/kernel/btf/vmlinux"
```

### Ожидаемые результаты

| Ядро | Ожидаемый результат |
|------|---------------------|
| 6.12.x (Debian 13) | ✅ Все тесты проходят, fentry/fexit |
| 6.8.x (Debian 12) | ✅ Все тесты проходят, fentry/fexit |
| 5.15.x (Ubuntu 22.04) | ✅ Все тесты проходят, kprobe |
| 4.19.x | ⚠️ Может потребоваться kprobe вместо fentry |

---

## 🔍 Устранение неполадок

### Тесты не запускаются

**Проблема**: `permission denied` или `operation not permitted`

**Решение**:
```bash
# Запуск от root
sudo go test -v ./tests/integration/...

# Или с capabilities
sudo setcap cap_sys_admin+ep $(which go)
```

### eBPF не загружается

**Проблема**: `failed to load eBPF program`

**Решение**:
```bash
# Проверка поддержки BTF
ls -la /sys/kernel/btf/vmlinux

# Проверка версий ядра на хостах
ssh root@192.168.5.99 "uname -r"
ssh root@192.168.5.214 "uname -r"

# Для старых ядер использовать kprobe
# Приложение автоматически переключается на kprobe
```

### Тесты зависают

**Проблема**: Тесты не завершаются в течение 5 минут

**Решение**:
```bash
# Увеличить таймаут
sudo go test -v -timeout 10m ./tests/integration/...

# Запустить с отладкой
sudo go test -v -run "TestConntrack" ./tests/integration/... 2>&1 | tee debug.log
```

### Разные результаты на хостах

**Проблема**: Тесты проходят на одном хосте, но не на другом

**Возможные причины**:
1. Разные версии ядра
2. Разные настройки безопасности (SELinux, AppArmor)
3. Разные настройки tracefs/eBPF

**Диагностика**:
```bash
# Сравнить версии ядра
ssh root@192.168.5.99 "uname -a"
ssh root@192.168.5.214 "uname -a"

# Проверить mount tracefs
ssh root@192.168.5.99 "mount | grep trace"
ssh root@192.168.5.214 "mount | grep trace"

# Проверить eBPF
ssh root@192.168.5.99 "bpftool btf dump | head -20"
ssh root@192.168.5.214 "bpftool btf dump | head -20"
```

---

## 📝 Пример отчета о тестировании

```
==========================================
TEST SUMMARY
==========================================
Host: 192.168.5.99
  Kernel: 6.12.85-1-pve
  OS: Debian GNU/Linux 13 (trixie)
  Tests: PASSED (11/11)
  
Host: 192.168.5.214
  Kernel: 6.8.12-8-pve
  OS: Debian GNU/Linux 12 (bookworm)
  Tests: PASSED (11/11)
==========================================

✅ Все тесты пройдены на обоих хостах
✅ Поддержка eBPF подтверждена
✅ Отслеживание входящих/исходящих подключений работает
✅ TCP handshake отслеживается корректно
```

---

## 📚 Дополнительные ресурсы

- [CONTRIBUTING.md](../../CONTRIBUTING.md) - Руководство по внесению изменений
- [README.md](../../README.md) - Основная документация
- [internal/conntrack/](../../internal/conntrack/) - Исходный код conntrack

---

*Последнее обновление: 2026-05-03*
