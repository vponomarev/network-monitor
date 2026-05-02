# Network Monitor — Быстрый старт

## 🚀 Установка и запуск

### Вариант 1: Netmon (мониторинг TCP потерь)

```bash
# 1. Скачать и установить
wget https://github.com/vponomarev/network-monitor/releases/latest/download/netmon-linux-amd64
chmod +x netmon-linux-amd64
sudo mv netmon-linux-amd64 /usr/local/bin/netmon

# 2. Mount tracefs (требуется для работы)
sudo mount -t tracefs none /sys/kernel/tracing

# 3. Запустить
sudo netmon

# 4. Проверить работу
curl http://localhost:9876/health
curl http://localhost:9876/metrics
```

**API Endpoints:**
- `GET /api/v1/loss/top?limit=10` — топ IP пар с потерями
- `GET /api/v1/discover/top` — топ путей с деталями
- `POST /api/v1/discover` — discovery для конкретной пары IP

---

### Вариант 2: Conntrack (трекинг соединений)

⚠️ **Важно:** Для работы conntrack требуется eBPF программа.

#### Шаг 1: Скачать бинарник

```bash
wget https://github.com/vponomarev/network-monitor/releases/latest/download/conntrack-linux-amd64
chmod +x conntrack-linux-amd64
sudo mv conntrack-linux-amd64 /usr/local/bin/conntrack
```

#### Шаг 2: Установить eBPF программу

**Способ A: Скачать из релиза (рекомендуется)**

```bash
sudo mkdir -p /usr/share/netmon/bpf
wget https://github.com/vponomarev/network-monitor/releases/download/v1.2.2/conntrack.bpf.o -O /usr/share/netmon/bpf/conntrack.bpf.o
```

**Способ B: Собрать из исходников**

```bash
# Требуется Linux с clang и llvm
sudo apt-get install -y clang llvm libbpf-dev linux-headers-$(uname -r)

git clone https://github.com/vponomarev/network-monitor.git
cd network-monitor
make -C bpf all

sudo mkdir -p /usr/share/netmon/bpf
sudo cp bpf/conntrack.bpf.o /usr/share/netmon/bpf/
```

#### Шаг 3: Запустить

```bash
# Запуск с настройками по умолчанию
sudo conntrack

# Или с явным указанием пути к eBPF
sudo conntrack --ebpf-prog /usr/share/netmon/bpf/conntrack.bpf.o
```

**API Endpoints:**
- `GET /api/v1/conntrack/connections?limit=10` — активные соединения
- `GET /api/v1/conntrack/stats` — статистика

---

## 📋 Конфигурация

### Netmon

Создайте `/etc/netmon/config.yaml`:

```yaml
global:
  ttl_hours: 3
  metrics_port: 9876
  trace_pipe_path: /sys/kernel/tracing/trace_pipe

metadata:
  locations:
    path: /etc/netmon/locations.yaml
  roles:
    path: /etc/netmon/roles.yaml

discovery:
  traceroute:
    enabled: true
    top_n: 10
    mode: both
    interval: 5m
    protocol: icmp

logging:
  level: info
  format: json
```

### Conntrack

Создайте `/etc/conntrack/config.yaml`:

```yaml
connections:
  enabled: true
  track_incoming: true
  track_outgoing: true
  track_closes: true
  filter_ports: []

logging:
  level: info
  format: json
  syslog:
    enabled: true
    tag: conntrack
    facility: LOCAL0
```

---

## 🐳 Docker

```bash
# Netmon
docker run --cap-add=SYS_ADMIN --cap-add=NET_RAW \
  -v /sys/kernel/tracing:/sys/kernel/tracing \
  -p 9876:9876 \
  ghcr.io/vponomarev/network-monitor/netmon:v1.2.2

# Conntrack
docker run --privileged \
  -p 9876:9876 \
  ghcr.io/vponomarev/network-monitor/conntrack:v1.2.2
```

---

## ✅ Проверка работы

### Netmon

```bash
# Health check
curl http://localhost:9876/health

# Metrics
curl http://localhost:9876/metrics | grep netmon_

# Top lossy pairs
curl http://localhost:9876/api/v1/loss/top?limit=5
```

### Conntrack

```bash
# Health check
curl http://localhost:9876/health

# Metrics
curl http://localhost:9876/metrics | grep conntrack_

# Active connections
curl http://localhost:9876/api/v1/conntrack/connections?limit=5
```

---

## 🛠️ Troubleshooting

### Netmon не запускается

```bash
# Проверить tracefs
ls -la /sys/kernel/tracing/trace_pipe

# Если нет — смонтировать
sudo mount -t tracefs none /sys/kernel/tracing
```

### Conntrack не загружает eBPF

```bash
# Проверить наличие eBPF программы
ls -la /usr/share/netmon/bpf/conntrack.bpf.o

# Проверить версию ядра (требуется 4.9+)
uname -r

# Проверить логи
sudo journalctl -u conntrack -f
```

### Ошибки eBPF

```bash
# Проверить загрузку модулей
lsmod | grep bpf

# Проверить dmesg
dmesg | grep -i bpf
```

---

## 📖 Документация

- [INSTALL.md](INSTALL.md) — Полное руководство по установке
- [README.md](README.md) — Общая информация о проекте
- [Configuration](docs/configuration.md) — Справка по конфигурации
- [Discovery API](docs/DISCOVERY_API.md) — API для path discovery
- [Conntrack Guide](docs/CONNTRACK.md) — Руководство по conntrack

---

## 🔐 Требования к правам

### Netmon
- `CAP_SYS_ADMIN` — Для доступа к trace_pipe
- `CAP_NET_RAW` — Для traceroute

### Conntrack
- `CAP_BPF` — Для eBPF программ
- `CAP_PERFMON` — Для eBPF perf events
- `CAP_NET_RAW` — Для raw socket access
- `CAP_SYS_ADMIN` — Для операций с ядром

**Рекомендуется запускать от root** (`sudo`) для полной функциональности.
