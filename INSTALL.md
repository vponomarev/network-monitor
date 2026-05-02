# Network Monitor — Руководство по установке

## 📦 Варианты установки

### 1. Быстрая установка (рекомендуется)

#### Netmon (TCP Loss Monitoring)

```bash
# Скачать бинарник
wget https://github.com/vponomarev/network-monitor/releases/latest/download/netmon-linux-amd64
chmod +x netmon-linux-amd64
sudo mv netmon-linux-amd64 /usr/local/bin/netmon

# Скачать конфигурацию
wget https://raw.githubusercontent.com/vponomarev/network-monitor/main/configs/config.example.yaml -O /etc/netmon/config.yaml

# Mount tracefs (требуется для trace_pipe)
sudo mount -t tracefs none /sys/kernel/tracing

# Запустить
sudo netmon --config /etc/netmon/config.yaml
```

#### Conntrack (Connection Tracking)

```bash
# Скачать бинарник
wget https://github.com/vponomarev/network-monitor/releases/latest/download/conntrack-linux-amd64
chmod +x conntrack-linux-amd64
sudo mv conntrack-linux-amd64 /usr/local/bin/conntrack

# Создать директорию для eBPF программ
sudo mkdir -p /usr/share/netmon/bpf

# Скачать eBPF программу
wget https://github.com/vponomarev/network-monitor/releases/latest/download/conntrack.bpf.o -O /usr/share/netmon/bpf/conntrack.bpf.o

# Скачать конфигурацию
sudo mkdir -p /etc/netmon
wget https://raw.githubusercontent.com/vponomarev/network-monitor/main/configs/config.example.yaml -O /etc/netmon/conntrack.yaml

# Запустить
sudo conntrack --config /etc/netmon/conntrack.yaml
```

---

### 2. Установка через скрипт (автоматическая)

```bash
# Netmon
curl -fsSL https://raw.githubusercontent.com/vponomarev/network-monitor/main/scripts/install-netmon.sh | sudo bash

# Conntrack
curl -fsSL https://raw.githubusercontent.com/vponomarev/network-monitor/main/scripts/install-conntrack.sh | sudo bash
```

---

### 3. Ручная установка (все компоненты)

#### Шаг 1: Проверка требований

```bash
# Проверка версии ядра (требуется 4.9+)
uname -r

# Проверка наличия tracefs (для netmon)
ls -la /sys/kernel/tracing/trace_pipe

# Проверка наличия eBPF (для conntrack)
ls -la /sys/fs/bpf/
```

#### Шаг 2: Установка netmon

```bash
# Создать директорию
sudo mkdir -p /opt/netmon /etc/netmon

# Скачать бинарник
cd /opt/netmon
sudo wget https://github.com/vponomarev/network-monitor/releases/latest/download/netmon-linux-amd64 -O netmon
sudo chmod +x netmon

# Скачать конфигурации
sudo wget https://raw.githubusercontent.com/vponomarev/network-monitor/main/configs/config.example.yaml -O /etc/netmon/config.yaml
sudo wget https://raw.githubusercontent.com/vponomarev/network-monitor/main/configs/locations.example.yaml -O /etc/netmon/locations.yaml
sudo wget https://raw.githubusercontent.com/vponomarev/network-monitor/main/configs/roles.example.yaml -O /etc/netmon/roles.yaml

# Mount tracefs
sudo mount -t tracefs none /sys/kernel/tracing

# Запустить
sudo /opt/netmon/netmon --config /etc/netmon/config.yaml
```

#### Шаг 3: Установка conntrack

```bash
# Создать директорию
sudo mkdir -p /opt/conntrack /usr/share/netmon/bpf /etc/netmon

# Скачать бинарник
cd /opt/conntrack
sudo wget https://github.com/vponomarev/network-monitor/releases/latest/download/conntrack-linux-amd64 -O conntrack
sudo chmod +x conntrack

# Скачать eBPF программу
sudo wget https://github.com/vponomarev/network-monitor/releases/latest/download/conntrack.bpf.o -O /usr/share/netmon/bpf/conntrack.bpf.o

# Скачать конфигурацию
sudo wget https://raw.githubusercontent.com/vponomarev/network-monitor/main/configs/config.example.yaml -O /etc/netmon/conntrack.yaml

# Запустить
sudo /opt/conntrack/conntrack --config /etc/netmon/conntrack.yaml
```

---

### 4. Установка из исходников

```bash
# Клонировать репозиторий
git clone https://github.com/vponomarev/network-monitor.git
cd network-monitor

# Установить зависимости
sudo apt-get install -y clang llvm libbpf-dev linux-headers-$(uname -r)

# Собрать eBPF программы
make -C bpf all

# Собрать бинарники
make build

# Установить
sudo make install
```

---

## 🔧 Конфигурация

### Netmon (config.yaml)

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

### Conntrack (conntrack.yaml)

```yaml
connections:
  enabled: true
  track_incoming: true
  track_outgoing: true
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

## 🚀 Запуск

### Netmon

```bash
# Проверка
sudo netmon --version

# Запуск с конфигом
sudo netmon --config /etc/netmon/config.yaml

# Проверка работы
curl http://localhost:9876/health
curl http://localhost:9876/metrics
```

### Conntrack

```bash
# Проверка
sudo conntrack --version

# Запуск с конфигом
sudo conntrack --config /etc/netmon/conntrack.yaml

# Проверка работы
curl http://localhost:9876/api/v1/conntrack/connections
```

---

## 📊 Проверка работы

### Prometheus Metrics

```bash
# Netmon metrics
curl http://localhost:9876/metrics | grep netmon_

# Conntrack metrics
curl http://localhost:9876/metrics | grep conntrack_
```

### API Endpoints

```bash
# Netmon API
curl http://localhost:9876/api/v1/loss/top?limit=5
curl http://localhost:9876/api/v1/discover/top

# Conntrack API
curl http://localhost:9876/api/v1/conntrack/connections?limit=10
curl http://localhost:9876/api/v1/conntrack/stats
```

---

## 🐛 Troubleshooting

### Netmon не запускается

```bash
# Проверить tracefs
mount -t tracefs none /sys/kernel/tracing

# Проверить права
ls -la /sys/kernel/tracing/trace_pipe
```

### Conntrack не загружает eBPF

```bash
# Проверить наличие eBPF программы
ls -la /usr/share/netmon/bpf/conntrack.bpf.o

# Проверить логи
sudo journalctl -u conntrack -f
```

### Ошибки eBPF

```bash
# Проверить версию ядра
uname -r

# Проверить наличие BPF filesystem
mount | grep bpf

# Проверить dmesg
dmesg | grep -i bpf
```

---

## 📖 Документация

- [README](README.md) — Общая информация
- [Configuration](docs/configuration.md) — Полная справка по конфигурации
- [Discovery API](docs/DISCOVERY_API.md) — API для path discovery
- [Conntrack Guide](docs/CONNTRACK.md) — Руководство по conntrack

---

## 🔐 Требования к правам

### Netmon
- `CAP_SYS_ADMIN` — Для доступа к trace_pipe
- `CAP_NET_RAW` — Для traceroute (ICMP/UDP/TCP)

### Conntrack
- `CAP_BPF` — Для eBPF программ
- `CAP_PERFMON` — Для eBPF perf events
- `CAP_NET_RAW` — Для raw socket access
- `CAP_SYS_ADMIN` — Для различных операций с ядром

**Рекомендуется запускать от root** (`sudo`) для полной функциональности.
