# Тест-отчёт: Conntrack Connection Tracking

## Дата: 2026-05-04

## Тестовая среда

| Параметр | Значение |
|----------|----------|
| Хост | debian13 (192.168.5.214) |
| ОС | Debian GNU/Linux 13 (trixie) |
| Ядро | 6.12.85+deb13-amd64 |
| BTF | ✅ Доступен |
| conntrack | v1.5.4 |

## Результаты тестов

### TC-001: eBPF Program Attachment

**Статус:** ✅ PASS

**Вывод:**
```
eBPF collection loaded successfully
Attached kprobe/tcp_connect for outgoing connections
Attached kretprobe/inet_csk_accept for incoming connections
Attached kprobe/tcp_close for connection closing
```

Все eBPF программы успешно прикреплены.

---

### TC-002: Outgoing Connection Tracking

**Статус:** ⚠️ FAIL (события не логируются)

**Симптомы:**
- kprobe/tcp_connect прикреплён успешно
- События исходящих соединений не появляются в логах
- Ring buffer reader не получает события

**Диагностика:**
```
=== Outgoing Connection Logs ===
{"level":"info","ts":...,"msg":"Attached kprobe/tcp_connect for outgoing connections"}
# Нет событий Parsed eBPF event
```

**Возможные причины:**
1. eBPF программа не отправляет события в ringbuf
2. Несоответствие структур Go и C
3. Проблема с чтением ringbuf

---

### TC-003: Incoming Connection Tracking

**Статус:** ⚠️ FAIL (события не логируются)

**Симптомы:**
- kretprobe/inet_csk_accept прикреплён успешно
- События входящих соединений не появляются в логах

---

### TC-004: Connection Close Tracking

**Статус:** ⏹️ NOT TESTED

Зависит от TC-002/TC-003

---

### TC-005: Process Identification

**Статус:** ⏹️ NOT TESTED

Зависит от TC-002/TC-003

---

## Анализ проблемы

### Наблюдения

1. **eBPF программы прикрепляются успешно** - все 4 программы (tcp_connect, inet_csk_accept, tcp_close, tracepoint fallback) прикрепляются без ошибок.

2. **События не поступают в Go** - ringbuf reader не получает события, хотя программы прикреплены.

3. **Проблема не в ядре** - ядро 6.12.85 новое, поддерживает eBPF, BTF доступен.

### Гипотезы

1. **Несоответствие структур:** Go struct `bpfConnectionEvent` может не соответствовать C struct `connection_event` в eBPF программе.

2. **Проблема с ringbuf:** eBPF программа может не отправлять события из-за ошибки в коде.

3. **CO-RE проблема:** eBPF программа может некорректно читать поля sock структуры.

### План исправления

1. Проверить соответствие структур Go и C
2. Добавить отладочные сообщения в eBPF программу
3. Проверить работу ringbuf через bpftool
4. Протестировать с упрощённой eBPF программой

## Следующие шаги

1. [ ] Добавить debug print в eBPF программу (bpf_printk)
2. [ ] Проверить bpftool prog show для run_count
3. [ ] Сверить размеры структур Go и C
4. [ ] Протестировать на другом ядре (Ubuntu 22.04, 5.15.x)

## Приложения

### Логи тестирования

```
=== eBPF Attachment Status ===
Loading eBPF program path=/usr/share/conntrack/bpf/conntrack.bpf.o
eBPF collection loaded successfully
Attached kprobe/tcp_connect for outgoing connections
Attached tracepoint/sock/inet_sock_set_state for outgoing connections (dual mode)
Attached kretprobe/inet_csk_accept for incoming connections
Attached kprobe/tcp_close for connection closing

=== Outgoing Connection Logs ===
# Нет событий Parsed eBPF event

=== Incoming Connection Logs ===
# Нет событий Parsed eBPF event
```

### Команды для диагностики

```bash
# Проверить прикрепленные программы
bpftool prog list | grep conntrack

# Проверить счётчики запусков
bpftool prog show | grep -E "name|run"

# Проверить ringbuf
bpftool map list | grep events

# Трассировка eBPF событий
cat /sys/kernel/debug/tracing/trace_pipe
```
