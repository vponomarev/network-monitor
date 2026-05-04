# Исправление проблемы с отслеживанием исходящих подключений

## 📋 Проблема

Перестали отслеживаться исходящие TCP подключения в conntrack.

## 🔍 Причина

Анализ показал следующие проблемы в `conntrack.bpf.c`:

### 1. Отсутствие проверки семейства сокетов в tcp_connect

**Было:**
```c
/* Note: No family check here - skc_family may not be set yet for outgoing.
 * IPv4/IPv6 filtering is done in extract_ipv4_addrs (returns 0.0.0.0 for IPv6).
 */
```

**Проблема:** Без проверки `family != AF_INET` eBPF программа обрабатывала IPv6 сокеты, что приводило к некорректным данным.

**Исправление:** Вернуть проверку семейства:
```c
// Check socket family - only IPv4 supported
__u16 family = BPF_CORE_READ(sk, __sk_common.skc_family);
if (family != AF_INET)
    return 0;
```

### 2. Fallback на inet_saddr в extract_ipv4_addrs

**Было:**
```c
// For tcp_connect, skc_rcv_saddr may be 0 (not bound yet)
// Fall back to inet_saddr from inet_sock
if (saddr4 == 0) {
    struct inet_sock *inet = (void *)sk;
    saddr4 = BPF_CORE_READ(inet, inet_saddr);
}
```

**Проблема:** `inet_saddr` также равен 0 до завершения `connect()`, fallback не помогает и вводит в заблуждение.

**Исправление:** Убрать fallback, принять что `src_ip = 0.0.0.0` для исходящих до bind() — это нормально.

### 3. Несовпадение ключей в map

**Проблема:**
- `tcp_connect` сохраняет key с `src_ip=0.0.0.0` (до bind)
- `tcp_close` ищет key с `src_ip=реальный_IP` (после bind)
- → события CLOSED для исходящих никогда не найдут запись в map

**Решение:** Userspace должен корректно обрабатывать `src_ip=0.0.0.0` для событий NEW.

---

## ✅ Выполненные исправления

### Файл: `bpf/conntrack.bpf.c`

#### 1. Исправлена функция `extract_ipv4_addrs`

```c
/* Extract IPv4 addresses from sock using BPF_CORE_READ
 * Uses skc_rcv_saddr/skc_daddr for all cases
 * Note: For outgoing connections before bind(), src_ip will be 0.0.0.0
 * This is expected behavior - userspace should handle this case
 */
static __always_inline void extract_ipv4_addrs(struct sock *sk, __u8 *saddr, __u8 *daddr)
{
    __u32 saddr4, daddr4;

    // Use skc_rcv_saddr/skc_daddr for all cases
    // For tcp_connect before bind(): saddr4 will be 0.0.0.0 (expected)
    saddr4 = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
    daddr4 = BPF_CORE_READ(sk, __sk_common.skc_daddr);

    // НЕТ fallback на inet_saddr
    // ...
}
```

#### 2. Исправлена функция `tcp_connect`

```c
/* Trace tcp_connect - outgoing connection initiation (SYN sent)
 * Socket family is already set at this point (socket() called before connect())
 */
SEC("kprobe/tcp_connect")
int BPF_KPROBE(tcp_connect, struct sock *sk)
{
    if (!track_outgoing)
        return 0;

    // Check socket family - only IPv4 supported
    __u16 family = BPF_CORE_READ(sk, __sk_common.skc_family);
    if (family != AF_INET)
        return 0;

    // ... остальной код
}
```

### Файл: `internal/conntrack/tracker_linux.go`

#### Добавлено расширенное логирование

```go
// Log all events for debugging (including src_ip=0.0.0.0)
t.logger.Debug("Parsed eBPF event",
    zap.String("src_ip", conn.SourceIP.String()),
    zap.String("dst_ip", conn.DestIP.String()),
    zap.Uint16("src_port", conn.SourcePort),
    zap.Uint16("dst_port", conn.DestPort),
    zap.String("direction", conn.Direction.String()),
    zap.String("state", conn.State.String()),
    zap.String("process", conn.ProcessName),
    zap.Uint32("pid", conn.PID),
)
```

---

## 🚀 Развертывание исправлений

### Вариант 1: Автоматическое развертывание

```bash
# Сборка и деплой на хост
./scripts/build-deploy-conntrack.sh 192.168.5.99
./scripts/build-deploy-conntrack.sh 192.168.5.214
```

### Вариант 2: Ручное развертывание

```bash
# 1. Копирование исходников на хост
scp -r bpf/ root@HOST:/tmp/conntrack-build/
scp -r cmd/conntrack/ root@HOST:/tmp/conntrack-build/
scp -r internal/conntrack/ root@HOST:/tmp/conntrack-build/
scp go.mod go.sum root@HOST:/tmp/conntrack-build/

# 2. Подключение к хосту
ssh root@HOST

# 3. Сборка eBPF
cd /tmp/conntrack-build/bpf
make all

# 4. Сборка Go
cd /tmp/conntrack-build
go build -o conntrack ./cmd/conntrack

# 5. Установка
cp conntrack /usr/local/bin/
cp bpf/*.o /usr/share/conntrack/bpf/

# 6. Перезапуск службы
systemctl restart conntrack
```

---

## 🧪 Тестирование

### Проверка исходящих подключений

```bash
# Запуск conntrack с debug логированием
sudo conntrack --config config.yaml --log-level debug

# В другом терминале - создание исходящего подключения
curl https://example.com

# Проверка логов
journalctl -u conntrack -f | grep "Parsed eBPF event"
```

### Ожидаемые логи

```
# Исходящее подключение (src_ip может быть 0.0.0.0 до bind)
"Parsed eBPF event" src_ip=0.0.0.0 dst_ip=93.184.216.34 direction=outgoing state=SYN_SENT

# После установления подключения
"Parsed eBPF event" src_ip=192.168.1.100 dst_ip=93.184.216.34 direction=outgoing state=ESTABLISHED

# Закрытие подключения
"Parsed eBPF event" src_ip=192.168.1.100 dst_ip=93.184.216.34 direction=outgoing state=CLOSED
```

### Проверка через API

```bash
# Получить активные подключения
curl http://localhost:9876/api/v1/conntrack/connections?direction=outgoing

# Статистика
curl http://localhost:9876/api/v1/conntrack/stats
```

---

## 📊 Метрики для проверки

```prometheus
# Количество исходящих подключений
conntrack_connections{direction="outgoing",state="established"}

# События
conntrack_events_total{event="NEW",direction="outgoing"}
conntrack_events_total{event="ESTABLISHED",direction="outgoing"}
conntrack_events_total{event="CLOSED",direction="outgoing"}
```

---

## ⚠️ Известные ограничения

1. **src_ip=0.0.0.0 для исходящих**: Для событий NEW src_ip может быть 0.0.0.0, если сокет не был bound до connect(). Это ожидаемое поведение.

2. **Несовпадение ключей**: Если tcp_connect сохранил запись с src_ip=0.0.0.0, а tcp_close ищет с реальным IP, запись не будет найдена. Это известная проблема, требующая дополнительной работы.

3. **Только IPv4**: Поддержка IPv6 требует дополнительной реализации.

---

## 📝 Чеклист проверки

- [ ] eBPF программа собирается без ошибок
- [ ] Исходящие подключения отслеживаются (события NEW)
- [ ] Входящие подключения отслеживаются
- [ ] События ESTABLISHED приходят для обоих направлений
- [ ] События CLOSED приходят для обоих направлений
- [ ] Метрики Prometheus обновляются
- [ ] API возвращает корректные данные
- [ ] Логи содержат информацию о src_ip=0.0.0.0 для исходящих

---

*Дата исправления: 2026-05-03*
*Версия: 1.0*
