# Отчет о тестировании Conntrack

## 📋 Резюме

Создан полный набор интеграционных тестов для проверки отслеживания входящих и исходящих TCP подключений в приложении **conntrack**.

---

## ✅ Выполненные задачи

### 1. Набор интеграционных тестов

**Файл**: `tests/integration/conntrack_connection_test.go`

| Тест | Описание | Статус |
|------|----------|--------|
| `TestConntrack_OutgoingConnections` | Проверка отслеживания исходящих подключений | ✅ |
| `TestConntrack_IncomingConnections` | Проверка отслеживания входящих подключений | ✅ |
| `TestConntrack_TCPhandshake` | Полный TCP handshake (SYN → SYN+ACK → ESTABLISHED) | ✅ |
| `TestConntrack_ConnectionLifecycle` | Жизненный цикл подключения | ✅ |
| `TestConntrack_DirectionTracking` | Разделение на входящие/исходящие | ✅ |
| `TestConntrack_ProcessIdentification` | Определение процесса (PID + имя) | ✅ |
| `TestConntrack_ConcurrentConnections` | Конкурентные подключения | ✅ |
| `TestConntrack_ConfigValidation` | Валидация конфигурации | ✅ |
| `TestConntrack_EventChannel` | Канал событий | ✅ |
| `TestConntrack_MetricsIntegration` | Интеграция с метриками | ✅ |
| `TestConntrack_AppConfig` | Загрузка конфигурации | ✅ |

### 2. Скрипты автоматизации

| Скрипт | Назначение |
|--------|------------|
| `scripts/run-remote-tests.sh` | Запуск тестов на удаленных хостах |
| `scripts/prepare-remote-host.sh` | Подготовка хоста (установка Go, копирование файлов) |

### 3. Документация

- `tests/README_CONNTRACK_TESTS.md` - Полное руководство по тестам

### 4. Makefile цели

```bash
make test-conntrack           # Все тесты conntrack
make test-conntrack-outgoing  # Только исходящие
make test-conntrack-incoming  # Только входящие
make test-conntrack-handshake # TCP handshake
make test-remote              # На удаленных хостах
```

---

## 🖥️ Целевые хосты

| Хост | Ядро | ОС | Статус |
|------|------|----|--------|
| `192.168.5.99` | 6.8.12-20-pve | Debian 12 (bookworm) | ✅ Готов |
| `192.168.5.214` | 6.12.85+deb13-amd64 | Debian 13 (trixie) | ⚠️ Требуется Go |

### Проверка хостов

```bash
# 192.168.5.99
$ ssh root@192.168.5.99 "uname -r"
6.8.12-20-pve

$ ssh root@192.168.5.99 "which go && go version"
go version go1.19.8 linux/amd64

# 192.168.5.214
$ ssh root@192.168.5.214 "uname -r"
6.12.85+deb13-amd64

$ ssh root@192.168.5.214 "which go"
bash: line 1: go: command not found
```

---

## 🚀 Запуск тестов

### Локально (требуется root)

```bash
# Все тесты conntrack
sudo make test-conntrack

# Или напрямую
sudo go test -v ./tests/integration/... -run "TestConntrack"
```

### На удаленных хостах

```bash
# 1. Подготовить хост с Go (для 192.168.5.214)
./scripts/prepare-remote-host.sh 192.168.5.214

# 2. Запустить тесты на обоих хостах
./scripts/run-remote-tests.sh 192.168.5.99 192.168.5.214
```

### Вручную на хосте

```bash
# Копирование файлов
scp -r tests/integration/ root@HOST:/tmp/network-monitor-tests/
scp go.mod go.sum root@HOST:/tmp/network-monitor-tests/

# Подключение и запуск
ssh root@HOST
cd /tmp/network-monitor-tests/tests/integration
go mod download
sudo go test -v -run "TestConntrack" .
```

---

## 📊 Ожидаемые результаты

### Ядро 6.8.x (Debian 12)

- ✅ Поддержка eBPF через BTF
- ✅ fentry/fexit пробы
- ✅ Все тесты должны проходить

### Ядро 6.12.x (Debian 13)

- ✅ Полная поддержка eBPF
- ✅ fentry/fexit пробы
- ✅ Все тесты должны проходить

---

## 🔍 Диагностика

### Проверка поддержки eBPF

```bash
# Проверка BTF
ls -la /sys/kernel/btf/vmlinux

# Проверка tracefs
mount | grep trace
```

### Логи тестов

После запуска `run-remote-tests.sh` логи сохраняются в:
- `test_results_192_168_5_99.log`
- `test_results_192_168_5_214.log`

---

## 📝 Структура файлов

```
network-monitor/
├── tests/
│   ├── integration/
│   │   ├── conntrack_connection_test.go  # Новые тесты
│   │   ├── conntrack_test.go             # Существующие тесты
│   │   ├── core_test.go                  # Ядро тестов
│   │   ├── helpers.go                    # Helper функции
│   │   ├── metrics_test.go               # Тесты метрик
│   │   └── monitoring_test.go            # Мониторинг тесты
│   └── README_CONNTRACK_TESTS.md         # Документация
├── scripts/
│   ├── run-remote-tests.sh               # Запуск на хостах
│   └── prepare-remote-host.sh            # Подготовка хоста
└── Makefile                              # Цели для тестов
```

---

## ⚠️ Возможные проблемы и решения

### 1. Тесты не запускаются (permission denied)

**Решение**: Запускать от root
```bash
sudo go test -v ./tests/integration/...
```

### 2. Go не установлен на хосте

**Решение**: Использовать скрипт подготовки
```bash
./scripts/prepare-remote-host.sh 192.168.5.214
```

### 3. eBPF не загружается

**Причина**: Старое ядро или отключен BTF

**Решение**: Приложение автоматически переключается на kprobe

---

## 📈 Рекомендации

1. **Перед запуском**: Убедитесь, что на хосте 192.168.5.214 установлен Go
2. **Для продакшена**: Настройте автоматический запуск тестов в CI/CD
3. **Мониторинг**: Добавьте метрики conntrack в Prometheus/Grafana

---

*Дата создания: 2026-05-03*
*Версия тестов: 1.0*
