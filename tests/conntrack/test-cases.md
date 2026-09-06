# Conntrack Test Cases

Подробная документация тест-кейсов для conntrack.

---

## Unit-тесты (internal/conntrack/)

### UT-001: Создание трекера

**Файл:** `internal/conntrack/tracker_linux_test.go::TestNewTracker`

**Цель:** Проверка создания трекера с конфигурацией

**Шаги:**
1. Создать конфиг с TrackIncoming=true, TrackOutgoing=true
2. Вызвать NewTracker()
3. Проверить, что трекер не nil
4. Проверить, что connections и events инициализированы

**Ожидаемый результат:** Трекер создан успешно, все поля инициализированы

---

### UT-002: Direction String

**Файл:** `internal/conntrack/tracker_linux_test.go::TestConnection_DirectionString`

**Цель:** Проверка String() для Direction

**Тесты:**
- DirectionIncoming → "incoming"
- DirectionOutgoing → "outgoing"
- Direction(99) → "unknown"

---

### UT-003: Генерация ключа подключения

**Файл:** `internal/conntrack/tracker_linux_test.go::TestTracker_connectionKey`

**Цель:** Проверка connectionKey()

**Ожидаемый результат:** Уникальный ключ для каждого подключения

---

### UT-004: Получение подключений

**Файл:** `internal/conntrack/tracker_linux_test.go::TestTracker_GetConnections`

**Цель:** Проверка GetConnections() и GetConnectionCount()

---

### UT-005: Канал событий

**Файл:** `internal/conntrack/tracker_linux_test.go::TestTracker_Events`

**Цель:** Проверка, что Events() возвращает не nil

---

### UT-006: Отправка события

**Файл:** `internal/conntrack/tracker_linux_test.go::TestTracker_sendEvent`

**Цель:** Проверка sendEvent()

---

### UT-007: Парсинг eBPF события

**Файл:** `internal/conntrack/tracker_linux_test.go::TestTracker_parseConnectionEvent`

**Цель:** Проверка parseConnectionEvent()

---

### UT-008: Остановка по контексту

**Файл:** `internal/conntrack/tracker_linux_test.go::TestTracker_Run_ContextCancellation`

**Цель:** Проверка остановки трекера при отмене контекста

---

### UT-009: Симуляция событий

**Файл:** `internal/conntrack/tracker_linux_test.go::TestTracker_simulateEvents`

**Цель:** Проверка simulateEvents()

---

### UT-010: Очистка имени процесса

**Файл:** `internal/conntrack/tracker_linux_test.go::Test_sanitizeProcessName`

**Цель:** Проверка sanitizeProcessName()

**Тесты:**
- "sshd" → "sshd"
- "sshd\x00\x00" → "sshd"
- "" → "unknown"
- "\x00\x00" → "unknown"
- "  nginx  " → "nginx"

---

### UT-011: Чтение /proc/{pid}/comm

**Файл:** `internal/conntrack/tracker_linux_test.go::Test_getProcessComm`

**Цель:** Проверка чтения comm текущего процесса

---

### UT-012: Невалидный PID

**Файл:** `internal/conntrack/tracker_linux_test.go::Test_getProcessComm_invalid`

**Цель:** Проверка обработки PID=0 и несуществующего PID

---

### UT-013: Обогащение имени процесса

**Файл:** `internal/conntrack/tracker_linux_test.go::Test_enrichProcessName`

**Цель:** Проверка enrichProcessName()

---

### UT-014: Выравнивание struct eBPF

**Файл:** `internal/conntrack/tracker_linux_test.go::Test_bpfConnectionEvent_StructAlignment`

**Цель:** Проверка соответствия Go struct C struct

**Требования:**
- Размер: 112 байт
- Comm offset: 72

---

### UT-015: Валидация struct eBPF

**Файл:** `internal/conntrack/tracker_linux_test.go::Test_validateBpfConnectionEvent`

**Цель:** Проверка validateBpfConnectionEvent()

---

### UT-016-UT-020: State Machine тесты

**Файл:** `internal/conntrack/state_machine_test.go`

| Тест | Цель |
|------|------|
| TestConnectionState_String | String() для состояний |
| TestConnectionEvent_String | String() для событий |
| TestConnection_Duration | Длительность подключения |
| TestConnection_HandshakeDuration | Длительность handshake |
| TestConnection_Direction | IsOutgoing/IsIncoming |

---

### UT-021-UT-025: State Machine логика

**Файл:** `internal/conntrack/state_machine_test.go`

| Тест | Цель |
|------|------|
| TestStateMachine_NewConnection | Обработка NEW |
| TestStateMachine_EstablishedConnection | Переход в ESTABLISHED |
| TestStateMachine_SYNTimeout | Таймаут SYN |
| TestStateMachine_GetAllConnections | Все подключения |
| TestMakeConnectionKey | Генерация ключа |

---

### UT-026-UT-032: Syslog Writer

**Файл:** `internal/conntrack/syslog_test.go`

| Тест | Цель |
|------|------|
| TestSyslogWriter_FormatMessage | Форматирование OUT |
| TestSyslogWriter_FormatIncomingConnection | Форматирование IN |
| TestSyslogWriter_FormatClosedConnection | Форматирование CLOSED |
| TestSyslogWriter_FormatFailedConnection | Форматирование FAILED |
| TestSyslogWriter_ProtocolString | Protocol.String() |
| TestSyslogWriter_WithHostname | Hostname в syslog |
| TestSyslogWriter_WithoutProcessName | Без process name |

---

## Интеграционные тесты (tests/conntrack/integration/)

### IT-001: Исходящие подключения

**Файл:** `tests/conntrack/integration/connection_test.go::TestConntrack_OutgoingConnections`

**Требует:** root

**Цель:** Проверка отслеживания исходящих TCP подключений

**Шаги:**
1. Запустить трекер с TrackOutgoing=true
2. Создать 5 подключений к localhost:80/22
3. Проверить количество отслеженных подключений

**Ожидаемый результат:** Подключения отслежены

---

### IT-002: Входящие подключения

**Файл:** `tests/conntrack/integration/connection_test.go::TestConntrack_IncomingConnections`

**Требует:** root

**Цель:** Проверка отслеживания входящих TCP подключений

**Шаги:**
1. Запустить трекер с TrackIncoming=true
2. Запустить TCP сервер на случайном порту
3. Создать 3 входящих подключения
4. Проверить отслеживание

---

### IT-003: Полный TCP handshake

**Файл:** `tests/conntrack/integration/connection_test.go::TestConntrack_TCPhandshake`

**Требует:** root

**Цель:** Проверка полного цикла SYN → SYN+ACK → ESTABLISHED

**Шаги:**
1. Запустить сервер
2. Клиент создаёт подключение
3. Отправить данные
4. Проверить состояние подключения

---

### IT-004: Разделение направлений

**Файл:** `tests/conntrack/integration/connection_test.go::TestConntrack_DirectionTracking`

**Требует:** root

**Цель:** Проверка разделения на incoming/outgoing

**Шаги:**
1. Создать 3 исходящих подключения
2. Создать 3 входящих подключения
3. Проверить подсчёт по направлениям

---

### IT-005: Конкурентные подключения

**Файл:** `tests/conntrack/integration/connection_test.go::TestConntrack_ConcurrentConnections`

**Требует:** root

**Цель:** Проверка работы при конкурентной нагрузке

**Шаги:**
1. Запустить сервер
2. Создать 10 конкурентных подключений
3. Проверить, что все отслежены

**Ожидаемый результат:** 10 подключений отслежены без потерь

---

### IT-006: Канал событий

**Файл:** `tests/conntrack/integration/connection_test.go::TestConntrack_EventChannel`

**Требует:** root

**Цель:** Проверка работы канала событий

**Шаги:**
1. Подписаться на Events()
2. Создать 3 подключения
3. Посчитать полученные события

---

### IT-007: Определение процесса

**Файл:** `tests/conntrack/integration/connection_test.go::TestConntrack_ProcessIdentification`

**Требует:** root

**Цель:** Проверка определения PID и имени процесса

---

### IT-008: Валидация конфигурации

**Файл:** `tests/conntrack/integration/connection_test.go::TestConntrack_ConfigValidation`

**Требует:** root

**Цель:** Проверка создания трекера с минимальной конфигурацией

---

### IT-009: Загрузка конфигурации из файла

**Файл:** `tests/conntrack/integration/connection_test.go::TestConntrack_AppConfig`

**Требует:** нет

**Цель:** Проверка загрузки конфигурации из YAML файла

---

## Матрица совместимости

| Тест | Ubuntu 22.04 (5.15) | Debian 12 (6.1) | Debian 13 (6.12) | Proxmox (6.8) |
|------|---------------------|-----------------|------------------|---------------|
| UT-001 - UT-032 | ✅ | ✅ | ✅ | ✅ |
| IT-001 | ✅ | ✅ | ✅ | ✅ |
| IT-002 | ✅ | ✅ | ✅ | ✅ |
| IT-003 | ✅ | ✅ | ✅ | ✅ |
| IT-004 | ✅ | ✅ | ✅ | ✅ |
| IT-005 | ✅ | ✅ | ✅ | ✅ |
| IT-006 | ✅ | ✅ | ✅ | ✅ |
| IT-007 | ✅ | ✅ | ✅ | ✅ |
| IT-008 | ✅ | ✅ | ✅ | ✅ |
| IT-009 | ✅ | ✅ | ✅ | ✅ |

---

## Запуск всех тестов

```bash
# Unit тесты (без root)
go test -v ./internal/conntrack/...

# Integration тесты (требуется root)
sudo go test -v ./tests/conntrack/integration/...

# Все тесты
go test -v ./internal/conntrack/... && sudo go test -v ./tests/conntrack/integration/...

# С покрытием
go test -v -cover ./internal/conntrack/...

# Конкретный тест
go test -v ./internal/conntrack/... -run TestNewTracker
sudo go test -v ./tests/conntrack/integration/... -run TestConntrack_OutgoingConnections
```

---

## Отчёт о результатах

После запуска на тестовых хостах заполните `TESTING_REPORT.md`:

```markdown
### Host: 192.168.5.217 (Ubuntu 22.04, kernel 5.15.0-177)

| Test | Status | Notes |
|------|--------|-------|
| UT-001 - UT-032 | PASS/FAIL | |
| IT-001 | PASS/FAIL | |
| IT-002 | PASS/FAIL | |
| IT-003 | PASS/FAIL | |
| IT-004 | PASS/FAIL | |
| IT-005 | PASS/FAIL | |
| IT-006 | PASS/FAIL | |
| IT-007 | PASS/FAIL | |
| IT-008 | PASS/FAIL | |
| IT-009 | PASS/FAIL | |
```
