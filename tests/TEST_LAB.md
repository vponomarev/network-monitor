# Test Lab — Хосты для тестирования

## Список тестовых хостов

| # | Hostname | ОС | Kernel | IP | Go Version | Go Path | Статус |
|---|----------|----|--------|-----|------------|---------|--------|
| 1 | ubuntu22 | Ubuntu 22.04 | 5.15.0-177-generic | 192.168.5.217 | 1.20.14 | /usr/local/go/bin/go | ✅ Готов |
| 2 | debian13 | Debian 13 (trixie) | 6.12.85+deb13-amd64 | 192.168.5.214 | 1.20.14 | /usr/local/go/bin/go | ✅ Готов |
| 3 | debian11 | Debian 11 (bullseye) | 6.1.0-45-amd64 | 192.168.5.193 | 1.20.14 | /usr/local/go/bin/go | ✅ Готов |
| 4 | fox | Debian 12 + Proxmox VE 8.4 | 6.8.12-20-pve | 192.168.5.99 | 1.20.14 | /usr/local/go/bin/go | ✅ Готов |

---

## Детальная информация о хостах

### 1. ubuntu22 (192.168.5.217)

| Параметр | Значение |
|----------|----------|
| **ОС** | Ubuntu 22.04 LTS |
| **Kernel** | 5.15.0-177-generic |
| **Архитектура** | x86_64 |
| **Go Version** | go1.20.14 linux/amd64 |
| **Go Path** | /usr/local/go/bin/go |
| **SSH** | `ssh root@192.168.5.217` |
| **eBPF** | kprobe/kretprobe (legacy) |

**Особенности:**
- Ядро 5.15 поддерживает eBPF через kprobe/kretprobe
- Требуется CAP_SYS_ADMIN для trace_pipe

---

### 2. debian13 (192.168.5.214)

| Параметр | Значение |
|----------|----------|
| **ОС** | Debian 13 (trixie) |
| **Kernel** | 6.12.85+deb13-amd64 |
| **Архитектура** | x86_64 |
| **Go Version** | go1.20.14 linux/amd64 |
| **Go Path** | /usr/local/go/bin/go |
| **SSH** | `ssh root@192.168.5.214` |
| **eBPF** | fentry/fexit (modern) |

**Особенности:**
- Современное ядро с полной поддержкой eBPF
- Поддержка fentry/fexit hook'ов
- BTF включён по умолчанию

---

### 3. debian11 (192.168.5.193)

| Параметр | Значение |
|----------|----------|
| **ОС** | Debian 11 (bullseye) |
| **Kernel** | 6.1.0-45-amd64 |
| **Архитектура** | x86_64 |
| **Go Version** | go1.20.14 linux/amd64 |
| **Go Path** | /usr/local/go/bin/go |
| **SSH** | `ssh root@192.168.5.193` |
| **eBPF** | kprobe/kretprobe + fentry |

**Особенности:**
- Ядро 6.1 с улучшенной поддержкой eBPF
- Возможна работа tracepoint/sock/inet_sock_set_state

---

### 4. fox (192.168.5.99)

| Параметр | Значение |
|----------|----------|
| **ОС** | Debian 12 + Proxmox VE 8.4 |
| **Kernel** | 6.8.12-20-pve |
| **Архитектура** | x86_64 |
| **Go Version** | go1.20.14 linux/amd64 |
| **Go Path** | /usr/local/go/bin/go |
| **SSH** | `ssh root@192.168.5.99` |
| **eBPF** | fentry/fexit (modern) |

**Особенности:**
- Proxmox VE kernel на базе 6.8
- Полная поддержка eBPF включая fentry/fexit
- BTF включён

---

## Go Compiler Information

### Версия Go

**Установленная версия:** go1.20.14 linux/amd64

**Причина выбора:**
- Минимальная версия для cilium/ebpf v0.12.3+
- Поддержка `unsafe.StringData` (Go 1.20+)
- Совместимость со всеми хостами

### Расположение Go

| Компонент | Путь |
|-----------|------|
| **Бинарный файл** | `/usr/local/go/bin/go` |
| **GOROOT** | `/usr/local/go` |
| **GOPATH** | `/root/go` (по умолчанию) |
| **PATH** | `/usr/local/go/bin` добавлен в `/etc/profile.d/go.sh` |

### Проверка установки

```bash
# Проверка версии
go version

# Проверка расположения
which go

# Проверка окружения
go env GOROOT GOPATH
```

### Установка (для справки)

```bash
# Скачать Go 1.20.14
wget https://go.dev/dl/go1.20.14.linux-amd64.tar.gz

# Установить
rm -rf /usr/local/go
tar -C /usr/local -xzf go1.20.14.linux-amd64.tar.gz

# Настроить PATH
echo 'export PATH=/usr/local/go/bin:$PATH' > /etc/profile.d/go.sh
chmod +x /etc/profile.d/go.sh
source /etc/profile.d/go.sh
```

---

## Подключение к хостам

### SSH команды

```bash
# Ubuntu 22.04
ssh root@192.168.5.217

# Debian 13
ssh root@192.168.5.214

# Debian 11
ssh root@192.168.5.193

# Proxmox VE
ssh root@192.168.5.99
```

### Массовое выполнение команд

```bash
# На всех хостах одновременно
for HOST in 192.168.5.217 192.168.5.214 192.168.5.193 192.168.5.99; do
  ssh root@$HOST "command"
done
```

---

## Требования для тестирования

### Системные требования

| Требование | Команда проверки |
|------------|------------------|
| **BTF включён** | `ls -la /sys/kernel/btf/vmlinux` |
| **Tracefs смонтирован** | `mount \| grep tracefs` |
| **BPF fs смонтирован** | `mount \| grep bpf` |
| **Root доступ** | `id` (должен быть uid=0) |

### Проверка окружения

```bash
# Проверка ядра
uname -r

# Проверка Go
go version
which go

# Проверка eBPF поддержки
ls -la /sys/kernel/btf/vmlinux
ls -la /sys/kernel/tracing/
```

---

## История изменений

| Дата | Изменение |
|------|-----------|
| 2026-05-04 | Обновлён Go до 1.20.14 на всех хостах |
| 2026-05-04 | Добавлена информация о Go compiler |
| 2026-05-04 | Добавлены hostname для всех хостов |

---

*Последнее обновление: 2026-05-04*
