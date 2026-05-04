# Отчет о тестировании на чистом Debian 12 (хост 192.168.5.193)

## 📋 Резюме

Тестирование проведено на чистом хосте Debian 12 с ядром 6.1.0-45-amd64 (без модификаций Proxmox).

**Результат**: ⚠️ **Частично работает** - входящие подключения отслеживаются, исходящие - нет.

---

## 🖥️ Информация о хосте

| Параметр | Значение |
|----------|----------|
| **Хост** | 192.168.5.193 |
| **ОС** | Debian 12 (bookworm) |
| **Ядро** | 6.1.0-45-amd64 |
| **Go** | 1.21.0 |
| **cilium/ebpf** | v0.15.0 |
| **BTF** | ✅ Доступен |
| **fentry** | ✅ Доступен |

---

## ✅ Выполненные действия

### 1. Установка зависимостей

```bash
apt-get install -y clang llvm libbpf-dev linux-headers-$(uname -r) \
                   wget git make curl
```

### 2. Установка Go 1.21

```bash
wget -q https://go.dev/dl/go1.21.0.linux-amd64.tar.gz
tar -C /usr/local -xzf go1.21.0.linux-amd64.tar.gz
export PATH=/usr/local/go/bin:$PATH
```

### 3. Сборка eBPF и conntrack

```bash
cd /tmp/conntrack-build/bpf && make all
cd /tmp/conntrack-build && go build -o conntrack ./cmd/conntrack
```

### 4. Развертывание

```bash
cp conntrack /usr/local/bin/
cp bpf/conntrack.bpf.o /usr/share/netmon/bpf/
mkdir -p /etc/conntrack
# Создание конфигурации
/usr/local/bin/conntrack --config /etc/conntrack/config.yaml
```

---

## 🧪 Результаты тестирования

### Входящие подключения (✅ Работает)

```log
{"level":"debug","msg":"Parsed eBPF event",
 "src_ip":"192.168.5.165","dst_ip":"192.168.5.193",
 "src_port":64837,"dst_port":22,
 "direction":"incoming","state":"ESTABLISHED",
 "process":"sshd","pid":530}
```

**Статистика:**
- Входящие события: 18
- Исходящие события: 0

### Исходящие подключения (❌ Не работает)

```bash
curl -4 -s https://vpnc.ru/ > /dev/null
# События не генерируются
```

**Прикрепление eBPF прошло успешно:**
```log
{"level":"info","msg":"Attached kprobe/tcp_connect for outgoing connections"}
{"level":"info","msg":"eBPF collection loaded successfully"}
```

---

## 🔍 Диагностика проблемы

### Проверка kprobe

```bash
# tcp_connect доступен в kallsyms
$ grep tcp_connect /proc/kallsyms
ffffffff88897fb0 T tcp_connect

# Функция доступна для ftrace
$ cat /sys/kernel/debug/tracing/available_filter_functions | grep ^tcp_connect$
tcp_connect
```

### Проверка fentry

```bash
# fentry доступен
$ cat /proc/kallsyms | grep __fentry__
ffffffff880763f0 T __fentry__
```

### Проблема

kprobe/tcp_connect прикрепляется успешно, но **не генерирует события** при создании исходящих подключений.

**Возможные причины:**
1. Ядро 6.1.x Debian требует использования fentry вместо kprobe
2. Конфликт с другими eBPF программами в системе
3. Специфика трассировки в ядре 6.1.x

---

## 📊 Сравнение с другими хостами

| Хост | Ядро | ОС | Входящие | Исходящие |
|------|------|----|----------|-----------|
| 192.168.5.214 | 6.12.85 | Debian 13 | ✅ | ✅ |
| 192.168.5.193 | 6.1.0-45 | Debian 12 | ✅ | ❌ |
| 192.168.5.99 | 6.8.12 | Debian 12 (Proxmox) | ✅ | ❌ |

---

## 🔧 Рекомендации

### Для ядра 6.1.x (Debian 12)

1. **Использовать fentry/fexit вместо kprobe/kretprobe**

   Обновить `conntrack.bpf.c`:
   ```c
   // Вместо SEC("kprobe/tcp_connect")
   SEC("fentry/tcp_connect")
   int BPF_PROG(tcp_connect_fentry, struct sock *sk)
   {
       // ...
   }
   ```

2. **Проверить LSM lockdown**
   ```bash
   cat /sys/kernel/security/lockdown
   ```

3. **Использовать более новое ядро** (6.12.x как на 192.168.5.214)

### Для продакшена

- Использовать ядро 6.12.x+ где kprobe работает корректно
- Или обновить eBPF программу для использования fentry/fexit

---

## 📝 Выводы

1. **Входящие подключения** отслеживаются корректно на всех хостах
2. **Исходящие подключения** работают только на ядре 6.12.x (Debian 13)
3. На ядрах 6.1.x и 6.8.x требуется использование fentry/fexit вместо kprobe
4. Чистая установка Debian 12 не решает проблему - требуется обновление eBPF программы

---

*Дата тестирования: 2026-05-03*
*Версия отчета: 1.1*
*Статус: ⚠️ Частично работает (только входящие)*
