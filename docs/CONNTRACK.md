# Connection Tracking (Conntrack) Module

Модуль отслеживания TCP-соединений на основе eBPF для мониторинга входящих и исходящих соединений с логированием в syslog и экспортом метрик Prometheus.

## Возможности

- **Отслеживание TCP handshake** - мониторинг трехэтапного рукопожатия (SYN → SYN+ACK → ESTABLISHED)
- **Входящие и исходящие соединения** - раздельный трекинг по направлению
- **Логирование в syslog** - структурированные сообщения в формате RFC 5424
- **Prometheus метрики** - экспорт статистики соединений
- **HTTP API** - REST API для просмотра активных соединений
- **eBPF на основе kernel probes** - минимальные накладные расходы

## Архитектура

```
┌─────────────────────────────────────────────────────────────┐
│                     Connection Tracker                       │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────────┐  │
│  │   eBPF      │  │   State      │  │    Syslog         │  │
│  │   Probes    │─▶│   Machine    │─▶│    Writer         │  │
│  │  (kprobes)  │  │  (TCP FSM)   │  │                   │  │
│  └─────────────┘  └──────────────┘  └───────────────────┘  │
│                          │                                  │
│                          ▼                                  │
│                   ┌──────────────┐  ┌───────────────────┐  │
│                   │   Metrics    │  │    HTTP API       │  │
│                   │   Collector  │  │    /api/v1/       │  │
│                   │              │  │    conntrack/     │  │
│                   └──────────────┘  └───────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## Компоненты

### 1. State Machine

Конечный автомат TCP-соединений управляет жизненным циклом соединения:

```
NEW → SYN_SENT → ESTABLISHED → CLOSED
              │
              └──(timeout)──→ FAILED
```

**Состояния:**
- `NEW` - новое соединение обнаружено
- `SYN_SENT` - SYN отправлен, ожидание SYN+ACK
- `SYN_RECEIVED` - SYN получен, ожидание accept
- `ESTABLISHED` - соединение установлено
- `CLOSING` - соединение закрывается (FIN sent)
- `CLOSED` - соединение закрыто

**События:**
- `EventNew` - новое соединение
- `EventEstablished` - соединение установлено
- `EventClosed` - соединение закрыто
- `EventFailed` - таймаут SYN (нет SYN+ACK)
- `EventRejected` - входящее соединение отклонено

### 2. Syslog Writer

Экспорт событий в syslog в структурированном формате:

```
CONN_OUT_ESTABLISHED src=192.168.1.100:54321 dst=8.8.8.8:443 proto=TCP dir=outgoing state=ESTABLISHED pid=1234 comm="curl" handshake_ms=50 host=server1 ts=2024-01-15T10:00:00Z
```

**Формат сообщений:**
- `CONN_OUT_NEW` - новое исходящее соединение
- `CONN_OUT_ESTABLISHED` - исходящее соединение установлено
- `CONN_IN_ACCEPTED` - входящее соединение принято
- `CONN_IN_REJECTED` - входящее соединение отклонено
- `CONN_OUT_FAILED` - исходящее соединение не удалось
- `CONN_CLOSED` - соединение закрыто

### 3. Prometheus Metrics

**Метрики:**

```prometheus
# Количество соединений по состоянию и направлению
conntrack_connections{state="established", direction=""} 150
conntrack_connections{state="pending_outgoing", direction="outgoing"} 5
conntrack_connections{state="pending_incoming", direction="incoming"} 3

# События соединений (счетчик)
conntrack_events_total{event="NEW", direction="outgoing"} 1000
conntrack_events_total{event="ESTABLISHED", direction="outgoing"} 950
conntrack_events_total{event="FAILED", direction="outgoing"} 50
conntrack_events_total{event="CLOSED", direction="outgoing"} 900

# Длительность TCP handshake (гистограмма)
conntrack_handshake_duration_seconds{direction="outgoing"}

# Длительность соединения (гистограмма)
conntrack_connection_duration_seconds{direction="outgoing"}
```

### 4. HTTP API

**Получить список соединений:**

```bash
GET /api/v1/conntrack/connections?limit=100&state=ESTABLISHED&direction=outgoing
```

**Параметры:**
- `limit` - максимальное количество записей (по умолчанию 100)
- `state` - фильтр по состоянию (NEW, SYN_SENT, ESTABLISHED, CLOSED)
- `direction` - фильтр по направлению (incoming, outgoing)

**Пример ответа:**

```json
[
  {
    "id": "192.168.1.100:54321:8.8.8.8:443:6",
    "src_ip": "192.168.1.100",
    "src_port": 54321,
    "dst_ip": "8.8.8.8",
    "dst_port": 443,
    "protocol": "TCP",
    "direction": "outgoing",
    "state": "ESTABLISHED",
    "pid": 1234,
    "process_name": "curl",
    "timestamp": "2024-01-15T10:00:00Z",
    "last_updated": "2024-01-15T10:00:01Z",
    "established": true,
    "duration": "1m30s",
    "handshake_time": "50ms"
  }
]
```

**Получить статистику:**

```bash
GET /api/v1/conntrack/stats
```

**Пример ответа:**

```json
{
  "total_outgoing": 150,
  "total_incoming": 50,
  "pending_outgoing": 5,
  "pending_incoming": 3,
  "established": 142,
  "total": 200
}
```

## Конфигурация

Добавьте в `config.yaml`:

```yaml
connections:
  enabled: true
  track_incoming: true
  track_outgoing: true
  filter_ports: []  # пустой = все порты
```

## Запуск

### Из исходного кода

```bash
# Сборка
make build

# Запуск (требуется root для eBPF)
sudo ./bin/netmon --config config.yaml
```

### Через conntrack CLI

```bash
# Запуск только conntrack
sudo ./bin/conntrack \
  --track-incoming \
  --track-outgoing \
  --syslog-tag conntrack \
  --syslog-facility LOCAL0
```

## Проверка работы

### 1. Проверка логов syslog

```bash
# Логи conntrack
sudo journalctl -t conntrack -f

# Или для LOCAL0
sudo journalctl -f SYSLOG_FACILITY=16  # LOCAL0 = 16
```

### 2. Проверка метрик

```bash
curl http://localhost:9876/metrics | grep conntrack
```

### 3. Проверка API

```bash
# Статистика
curl http://localhost:9876/api/v1/conntrack/stats

# Список соединений
curl http://localhost:9876/api/v1/conntrack/connections?limit=10
```

## eBPF Probes

Модуль использует следующие kernel probes:

| Probe | Тип | Назначение |
|-------|-----|------------|
| `tcp_connect` | kprobe | Исходящие соединения (SYN sent) |
| `tcp_v4_rcv` | kprobe | Входящие пакеты (SYN detection) |
| `tcp_v4_accept` | kprobe | Accept входящего соединения |
| `tcp_close` | kprobe | Закрытие соединения |
| `inet_sock_set_state` | tracepoint | Изменения состояния сокета |

## Требования

- Linux kernel 4.9+ (с поддержкой eBPF)
- Смонтированный tracefs: `mount -t tracefs none /sys/kernel/tracing`
- Root доступ или CAP_BPF, CAP_PERFMON capabilities
- Syslog daemon (rsyslog, syslog-ng)

## Troubleshooting

### eBPF программы не загружаются

```bash
# Проверка версии ядра
uname -r

# Проверка поддержки eBPF
zgrep CONFIG_BPF /proc/config.gz

# Проверка монтирования tracefs
mount | grep tracefs
```

### Нет событий в syslog

```bash
# Проверка работы syslog
sudo systemctl status rsyslog

# Проверка логов
sudo journalctl -u rsyslog -f

# Тестовое сообщение
logger -t conntrack-test "Test message"
```

### Метрики не экспортируются

```bash
# Проверка доступности метрик
curl http://localhost:9876/metrics | grep conntrack

# Проверка логов приложения
sudo journalctl -u netmon -f
```

## Структура модуля

```
internal/conntrack/
├── types.go              # Типы данных (Connection, Direction, State)
├── state_machine.go      # Конечный автомат TCP
├── state_machine_test.go # Тесты state machine
├── syslog.go             # Syslog writer
├── syslog_test.go        # Тесты syslog writer
├── tracker_linux.go      # Основной tracker (Linux)
├── tracker_other.go      # Заглушка для не-Linux
├── metrics.go            # Prometheus метрики
├── api.go                # HTTP API handler
└── api_test.go           # Тесты API
```

## Лицензия

MIT License
