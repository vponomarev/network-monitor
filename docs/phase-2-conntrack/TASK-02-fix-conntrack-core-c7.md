# TASK-02 — (C-7) Починить CO-RE conntrack (`inet_sock_set_state`) и загрузку на 6.8/6.12/6.17

**Метка исполнителя:** 🧠 strong 🐧 linux-host
**Зависит от:** TASK-01 (общий `bpf/vmlinux.h`, исправленный `trace_entry` и паттерн прямого чтения)
**Блокирует:** TASK-11 (блокирующий smoke-load conntrack в CI)
**Оценка:** ~0.5–1 день

---

## Контекст (проблема)

`conntrack.bpf.o` **не грузится** на ядрах 6.8/6.12 (и, ожидаемо, 6.17):
```
failed to resolve CO-RE relocation <byte_off> struct trace_event_raw_inet_sock_set_state.saddr
libbpf: prog 'trace_outgoing': failed to load: -22
```

**Корень:** рукописная структура в `bpf/vmlinux.h:290-302` объявляет поля как
`__u32`, а в реальном BTF ядра они другого типа/размера, и **`saddr`/`daddr` —
массивы `__u8[4]`, а не `__u32`**. CO-RE-релокация `<byte_off>` требует
совместимости типов локального и целевого поля; **`__u32` vs `__u8[4]`
несовместимы** → libbpf отклоняет загрузку. Плюс отсутствует поле
`const void *skaddr`, а `struct trace_entry` был 24 байта (чинится в TASK-01).

Текущая (неверная) структура `bpf/vmlinux.h:290-302`:
```c
struct trace_event_raw_inet_sock_set_state {
    struct trace_entry ent;
    __u32 oldstate;
    __u32 newstate;
    __u32 sport;
    __u32 dport;
    __u32 family;
    __u32 protocol;
    __u32 saddr;        // ← должно быть __u8 saddr[4]; нет поля skaddr
    __u32 daddr;
    __u32 saddr_v6[4];
    __u32 daddr_v6[4];
};
```

**Как conntrack читает контекст** (`bpf/conntrack.bpf.c`):
- `SEC("tracepoint/sock/inet_sock_set_state")`, `struct trace_event_raw_inet_sock_set_state *ctx` — строки 217-218
- `BPF_CORE_READ(ctx, protocol)` — 224; `BPF_CORE_READ(ctx, newstate)` — 226; `BPF_CORE_READ(ctx, family)` — 228
- `bpf_core_read(&saddr_bytes, sizeof(saddr_bytes), &ctx->saddr)` — 253 ← падающая релокация
- `bpf_core_read(&daddr_bytes, sizeof(daddr_bytes), &ctx->daddr)` — 255
- прямые чтения `(__u32)ctx->sport` / `ctx->dport` — 259, 277-278 (используют «зашитые» неверные смещения)

## Что сделать

1. **Исправить структуру** `trace_event_raw_inet_sock_set_state` в `bpf/vmlinux.h`
   по реальному формату (`include/trace/events/sock.h`, шаблон
   `inet_sock_set_state`):
   ```c
   struct trace_event_raw_inet_sock_set_state {
       struct trace_entry ent;   // 0   (8, после фикса TASK-01)
       const void *skaddr;       // 8   (8)  ← ДОБАВИТЬ (в текущей версии отсутствует)
       int   oldstate;           // 16  (4)
       int   newstate;           // 20  (4)
       __u16 sport;              // 24  (2)
       __u16 dport;              // 26  (2)
       __u16 family;             // 28  (2)
       __u16 protocol;           // 30  (2)  ← __u16, не __u32
       __u8  saddr[4];           // 32  (4)  ← массив, не __u32
       __u8  daddr[4];           // 36  (4)
       __u8  saddr_v6[16];       // 40  (16)
       __u8  daddr_v6[16];       // 56  (16)
       char  __data[0];
   };
   ```
   Сверь с `format`-файлом на целевом хосте (см. «Как проверить»).

2. **Перевести чтение на прямой доступ** в `bpf/conntrack.bpf.c` (по образцу
   TASK-01): `BPF_CORE_READ(ctx, X)` → `ctx->X`; `bpf_core_read(&dst, n, &ctx->saddr)`
   → почленное/`__builtin_memcpy` копирование `ctx->saddr`. Привести чтения
   `sport/dport` к типу `__u16` (сейчас часть читается как `__u32` — из-за этого
   и «работало» на старых ядрах со сдвинутыми смещениями). Проверить, что порядок
   байт портов сохраняется как раньше (ABI — big-endian порты; см. текущую
   обработку и не менять семантику метрик).

3. **kprobe-ветки** (`kprobe/tcp_connect`, `kretprobe/inet_csk_accept`,
   `kprobe/tcp_close`) читают из `struct sock` через `BPF_CORE_READ(sk, __sk_common...)`
   — это **другой** механизм (CO-RE по `struct sock_common`, который в BTF есть).
   **Не трогать** в этой задаче; если и там всплывёт релокация — вынести в APPENDIX.

4. **Пересобрать** и обновить embedded-объект:
   ```bash
   make -C bpf all
   cp bpf/conntrack.bpf.o pkg/embedded/bpf/
   ```

5. **Не сломать Go-парсинг.** Go-структура `bpfConnectionEvent`
   (`internal/conntrack/tracker_linux.go:63-79`, 88 байт, `Comm` @72) описывает
   **выходное** событие, не tracepoint-контекст. Меняться не должна. Прогони
   `go test -run 'Validate|StructAlignment|parseConnectionEvent' ./internal/conntrack/...`.

## Критерии приёмки (Definition of Done)

1. `trace_event_raw_inet_sock_set_state` в `bpf/vmlinux.h` имеет корректные типы
   (`__u16` для sport/dport/family/protocol, `__u8[4]` для saddr/daddr) и поле
   `const void *skaddr`.
2. `conntrack.bpf.c` не использует CO-RE для чтения полей tracepoint-контекста.
3. `bpftool prog loadall bpf/conntrack.bpf.o` **успешно грузится** на **6.8, 6.12,
   6.17** (и не ломается на 5.15/6.1).
4. Реальные исходящие соединения фиксируются: события приходят из ringbuf,
   `parseConnectionEvent` даёт корректные IP/порты/направление (проверить на VM).
5. `pkg/embedded/bpf/conntrack.bpf.o` пересобран и закоммичен.
6. `go build ./... && go test ./... && gofmt -l .` — чисто.

## Как проверить
```bash
sudo cat /sys/kernel/tracing/events/sock/inet_sock_set_state/format   # сверить ABI
make -C bpf all
sudo bpftool prog loadall bpf/conntrack.bpf.o /sys/fs/bpf/_c
sudo bpftool prog show | grep -E 'tracepoint|kprobe'
sudo rm -rf /sys/fs/bpf/_c
# Функциональная проверка: запустить conntrack, инициировать исходящее соединение
sudo ./conntrack --config /etc/conntrack/config.yaml &   # или встроенный в netmon
curl -s http://example.com >/dev/null
# убедиться, что соединение появилось (лог/метрики/API /api/v1/conntrack/connections)
```

## Риски
- Порядок байт портов: в tracepoint `sport/dport` хранятся в host-order или BE?
  Сверься с `format` и текущей семантикой (не изменить значения меток).
- После фикса `saddr[4]` копировать как байты в IPv4-mapped IPv6 так же, как
  сейчас (`bpf/conntrack.bpf.c:109-128`), чтобы Go-парсер не сломался.
- Если на 6.17 формат sock-трейспоинта отличается — задокументировать и,
  при необходимости, fallback на чтение из `struct sock`.
