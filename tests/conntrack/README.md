# Conntrack Test Suite

Тесты для модуля отслеживания TCP-соединений на основе eBPF.

## Структура тестов

```
tests/conntrack/
├── README.md                    # Этот файл
├── test-cases.md                # Документация тест-кейсов
└── integration/                 # Интеграционные тесты (требуют root)
    ├── connection_test.go       # Тесты подключений
    └── helpers.go               # Вспомогательные функции
```

**Unit-тесты** находятся в `internal/conntrack/*_test.go`:
- `tracker_linux_test.go` - тесты трекера
- `state_machine_test.go` - тесты state machine  
- `syslog_test.go` - тесты syslog writer

---

## Запуск тестов

### Unit-тесты (не требуют root)

```bash
# Все unit-тесты conntrack
go test -v ./internal/conntrack/... -run '^Test[^I]'

# Конкретные тесты
go test -v ./internal/conntrack/... -run TestNewTracker
go test -v ./internal/conntrack/... -run TestStateMachine
go test -v ./internal/conntrack/... -run TestSyslogWriter
```

### Интеграционные тесты (требуют root)

```bash
# Все интеграционные тесты
sudo go test -v ./tests/conntrack/integration/...

# Отдельные тесты
sudo go test -v ./tests/conntrack/integration/... -run TestConntrack_OutgoingConnections
sudo go test -v ./tests/conntrack/integration/... -run TestConntrack_IncomingConnections
sudo go test -v ./tests/conntrack/integration/... -run TestConntrack_TCPhandshake
```

### Все тесты conntrack

```bash
# Unit + integration (требуется root для integration)
go test -v ./internal/conntrack/... && sudo go test -v ./tests/conntrack/integration/...
```

---

## Список тест-кейсов

### Unit-тесты (internal/conntrack/)

| ID | Тест | Файл | Описание |
|----|------|------|----------|
| **UT-001** | `TestNewTracker` | tracker_linux_test.go | Создание трекера с конфигурацией |
| **UT-002** | `TestConnection_DirectionString` | tracker_linux_test.go | String() для Direction |
| **UT-003** | `TestTracker_connectionKey` | tracker_linux_test.go | Генерация ключа подключения |
| **UT-004** | `TestTracker_GetConnections` | tracker_linux_test.go | Получение списка подключений |
| **UT-005** | `TestTracker_Events` | tracker_linux_test.go | Проверка канала событий |
| **UT-006** | `TestTracker_sendEvent` | tracker_linux_test.go | Отправка события |
| **UT-007** | `TestTracker_parseConnectionEvent` | tracker_linux_test.go | Парсинг eBPF события |
| **UT-008** | `TestTracker_Run_ContextCancellation` | tracker_linux_test.go | Остановка по контексту |
| **UT-009** | `TestTracker_simulateEvents` | tracker_linux_test.go | Симуляция событий |
| **UT-010** | `Test_sanitizeProcessName` | tracker_linux_test.go | Очистка имени процесса |
| **UT-011** | `Test_getProcessComm` | tracker_linux_test.go | Чтение /proc/{pid}/comm |
| **UT-012** | `Test_getProcessComm_invalid` | tracker_linux_test.go | Невалидный PID |
| **UT-013** | `Test_enrichProcessName` | tracker_linux_test.go | Обогащение имени процесса |
| **UT-014** | `Test_bpfConnectionEvent_StructAlignment` | tracker_linux_test.go | Выравнивание struct eBPF |
| **UT-015** | `Test_validateBpfConnectionEvent` | tracker_linux_test.go | Валидация struct |
| **UT-016** | `TestConnectionState_String` | state_machine_test.go | String() для состояний TCP |
| **UT-017** | `TestConnectionEvent_String` | state_machine_test.go | String() для событий TCP |
| **UT-018** | `TestConnection_Duration` | state_machine_test.go | Длительность подключения |
| **UT-019** | `TestConnection_HandshakeDuration` | state_machine_test.go | Длительность handshake |
| **UT-020** | `TestConnection_Direction` | state_machine_test.go | IsOutgoing/IsIncoming |
| **UT-021** | `TestStateMachine_NewConnection` | state_machine_test.go | Обработка NEW |
| **UT-022** | `TestStateMachine_EstablishedConnection` | state_machine_test.go | Переход в ESTABLISHED |
| **UT-023** | `TestStateMachine_SYNTimeout` | state_machine_test.go | Таймаут SYN |
| **UT-024** | `TestStateMachine_GetAllConnections` | state_machine_test.go | Все подключения |
| **UT-025** | `TestMakeConnectionKey` | state_machine_test.go | Генерация ключа |
| **UT-026** | `TestSyslogWriter_FormatMessage` | syslog_test.go | Форматирование OUT |
| **UT-027** | `TestSyslogWriter_FormatIncomingConnection` | syslog_test.go | Форматирование IN |
| **UT-028** | `TestSyslogWriter_FormatClosedConnection` | syslog_test.go | Форматирование CLOSED |
| **UT-029** | `TestSyslogWriter_FormatFailedConnection` | syslog_test.go | Форматирование FAILED |
| **UT-030** | `TestSyslogWriter_ProtocolString` | syslog_test.go | Protocol.String() |
| **UT-031** | `TestSyslogWriter_WithHostname` | syslog_test.go | Hostname в syslog |
| **UT-032** | `TestSyslogWriter_WithoutProcessName` | syslog_test.go | Без process name |

### Интеграционные тесты (tests/conntrack/integration/)

| ID | Тест | Файл | Описание | Требует root |
|----|------|------|----------|--------------|
| **IT-001** | `TestConntrack_OutgoingConnections` | connection_test.go | Исходящие подключения | ✅ |
| **IT-002** | `TestConntrack_IncomingConnections` | connection_test.go | Входящие подключения | ✅ |
| **IT-003** | `TestConntrack_TCPhandshake` | connection_test.go | Полный handshake | ✅ |
| **IT-004** | `TestConntrack_DirectionTracking` | connection_test.go | Разделение направлений | ✅ |
| **IT-005** | `TestConntrack_ConcurrentConnections` | connection_test.go | 10 конкурентных подключений | ✅ |
| **IT-006** | `TestConntrack_EventChannel` | connection_test.go | Канал событий | ✅ |
| **IT-007** | `TestConntrack_ProcessIdentification` | connection_test.go | Определение процесса | ✅ |
| **IT-008** | `TestConntrack_ConfigValidation` | connection_test.go | Валидация конфигурации | ✅ |
| **IT-009** | `TestConntrack_AppConfig` | connection_test.go | Загрузка из config.yaml | ✅ |

---

## Тестовые хосты

Тесты предназначены для запуска на следующих хостах:

| Хост | ОС | Kernel | IP |
|------|----|--------|-----|
| Ubuntu 22.04 | Ubuntu 22.04 | 5.15.0-177 | 192.168.5.217 |
| Debian 13 | Debian 13 | 6.12.85 | 192.168.5.214 |
| Debian 12 | Debian 12 | 6.1.0-45 | 192.168.5.193 |
| Proxmox 8.4 | Debian 12 + PVE | 6.8.12-20-pve | 192.168.5.99 |

## Требования

### Unit-тесты
- Go 1.21+
- Библиотеки: `stretchr/testify`, `uber-go/zap`

### Интеграционные тесты
- Все требования unit-тестов
- Root доступ (eBPF требует CAP_BPF, CAP_PERFMON)
- Linux kernel 4.9+ с поддержкой eBPF
- Смонтированный BPF filesystem: `/sys/fs/bpf`

## Известные ограничения

1. **eBPF программы**: Интеграционные тесты используют simulation mode при отсутствии eBPF программ
2. **Порты**: Тесты используют localhost:80 и localhost:22, которые должны быть доступны
3. **Таймауты**: Некоторые тесты могут требовать увеличения таймаутов на медленных системах

## Добавление новых тестов

1. Unit-тесты: добавить в `internal/conntrack/*_test.go`
2. Интеграционные тесты: добавить в `tests/conntrack/integration/*_test.go`
3. Обновить этот README с новым тест-кейсом в таблице

## Отчёт о тестировании

После запуска тестов на хостах обновите `TESTING_REPORT.md` с результатами.
