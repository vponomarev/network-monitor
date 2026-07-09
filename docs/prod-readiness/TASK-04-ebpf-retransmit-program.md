# TASK-04 — eBPF-программа на tcp_retransmit_skb с ring buffer

> ✅ **СТАТУС: ВЫПОЛНЕНО** (основная сессия). Реализовано:
> - `bpf/tcploss.bpf.c` — `SEC("tracepoint/tcp/tcp_retransmit_skb")`, ring buffer `loss_events` (256KiB), IPv4-only (`family==AF_INET`), без `bpf_printk`.
> - Структура `tcploss_event` (48 байт, фикс. layout) читается через CO-RE (`trace_event_raw_tcp_event_sk_skb`, добавлен в `bpf/vmlinux.h`; имена полей совпадают с BTF ядра → офсеты релоцируются). Порты host-order, адреса — network-order в IPv4-mapped.
> - `bpf/Makefile`: `tcploss.bpf.c` добавлен в `SRCS`; собран clang 19 на debian13; `.o` (5480 б) лежит в `pkg/embedded/bpf/tcploss.bpf.o`.
> - **Cross-kernel load-тест (bpftool): OK на 5.15, 6.1, 6.8, 6.12** — одинаковый prog tag, CO-RE релоцирует под каждое ядро.

**Метка исполнителя:** 🧠 strong 🐧 linux-host
**Зависит от:** нет (но результат потребляется TASK-05)
**Оценка:** 1–2 дня (включая проверку на реальных ядрах)

---

## Контекст (зачем)

Сейчас netmon собирает потери TCP, **читая текст из `/sys/kernel/tracing/trace_pipe`**
и разбирая строки регэкспом (`internal/collector/trace_pipe.go`). Это непригодно для прода:

- `trace_pipe` — **единственный глобальный потребитель**: любой другой инструмент,
  второй экземпляр netmon или отладка ядра «крадут» события. Данные молча теряются.
- Текстовый парсинг хрупок (форматы строк меняются между ядрами).
- Нет backpressure и нет видимости потерь событий.
- Наш же conntrack пишет `bpf_printk` в тот же trace_pipe, засоряя источник.

Правильный production-путь — прицепить eBPF-программу к трейспоинту
`tracepoint/tcp/tcp_retransmit_skb` и отдавать **структурированные** события через
**BPF ring buffer**, как это уже сделано для conntrack.

**Референс, на который нужно опираться:** `bpf/conntrack.bpf.c` уже реализует ровно
этот паттерн (ring buffer `events`, извлечение IPv4-адресов из `struct sock`,
CO-RE через `vmlinux.h`, лицензия, сборка через `bpf/Makefile`). Скопируй стиль и
инфраструктуру оттуда.

## Что сделать

### 1. Новый eBPF-исходник `bpf/tcploss.bpf.c`

Создай программу, которая:

- Цепляется к `SEC("tracepoint/tcp/tcp_retransmit_skb")`.
- Читает контекст трейспоинта. Структура контекста в ядре —
  `struct trace_event_raw_tcp_event_sk_skb` (есть в `bpf/vmlinux.h`; проверь точное
  имя: `grep -n "tcp_event_sk_skb\|tcp_retransmit" bpf/vmlinux.h`). Поля включают
  `saddr[4]`, `daddr[4]` (IPv4, network byte order), `saddr_v6[16]`, `daddr_v6[16]`,
  `sport`, `dport`, `family`.
  > Дополнительно сверься с рантайм-форматом:
  > `cat /sys/kernel/tracing/events/tcp/tcp_retransmit_skb/format`
- Фильтрует только `family == AF_INET` (IPv4) на этом этапе. IPv6 — вне scope
  (задокументируй ограничение комментарием, как в conntrack.bpf.c).
- Заполняет событие и кладёт его в ring buffer через `bpf_ringbuf_reserve` /
  `bpf_ringbuf_submit` (см. `submit_event` в conntrack.bpf.c).
- **НЕ содержит `bpf_printk`** в hot-path (в отличие от conntrack.bpf.c — там это
  известная проблема, здесь не повторять).

### 2. Структура события (общая с userspace)

Определи C-структуру с явным выравниванием и фиксированными размерами. Рекомендуемый
макет (network-byte-order адреса храним как IPv4-mapped 16 байт, как в conntrack —
для единообразия парсинга в Go):
```c
struct tcploss_event {
    __u64 timestamp_ns;   // offset 0
    __u8  src_ip[16];     // offset 8  (IPv4-mapped IPv6)
    __u8  dst_ip[16];     // offset 24
    __u16 src_port;       // offset 40
    __u16 dst_port;       // offset 42
    __u8  family;         // offset 44 (AF_INET=2)
    __u8  _pad[3];        // offset 45..47 — выравнивание до 8
};                        // total: 48 байт
```
> Точный размер и офсеты ЗАФИКСИРУЙ в комментарии — Go-сторона (TASK-05) обязана
> побайтно совпадать. Используй тот же подход валидации размера, что в
> `validateBpfConnectionEvent` (`internal/conntrack/tracker_linux.go`).
> `src_port`/`dst_port` приведи к host byte order на userspace-стороне или в eBPF —
> зафиксируй, где именно, в комментарии (в conntrack dport конвертируется
> `bpf_ntohs`). Будь единообразен и явно опиши соглашение.

Извлечение IPv4-байт из `saddr[4]`/`daddr[4]` делай через `bpf_core_read`, как в
`trace_outgoing` в conntrack.bpf.c (строки ~247–275): читаешь 4 байта и раскладываешь
в `ip[12..15]`, выставляя `ip[10]=0xff; ip[11]=0xff`.

### 3. Ring buffer map

```c
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} loss_events SEC(".maps");
```
Имя map (`loss_events`) зафиксируй — оно используется в TASK-05.

### 4. Сборка

- Добавь `bpf/tcploss.bpf.c` в `bpf/Makefile` рядом с `conntrack.bpf.c` (посмотри,
  как там устроена цель сборки `.o`; повтори для нового файла).
- Собери: результат должен появиться, например, как `bpf/tcploss.bpf.o`.
- Положи собранный `.o` туда, откуда его подхватывает `pkg/embedded` (см.
  `pkg/embedded/bpf/`). В TASK-05 будет добавлен `//go:embed` для него.
  > Прочитай `pkg/embedded/embed.go` — там `//go:embed bpf/conntrack.bpf.o`.
  > Тебе понадобится аналогичный embed для нового `.o` (это делается в TASK-05,
  > но файл `.o` должен лежать в `pkg/embedded/bpf/tcploss.bpf.o`).

### 5. Ручная проверка на Linux-хосте

```bash
# собрать eBPF
make -C bpf all         # или конкретную цель для tcploss
# проверить, что программа валидна и грузится (нужен root, ядро 5.14+, BTF)
sudo bpftool prog load bpf/tcploss.bpf.o /sys/fs/bpf/tcploss   # или через tests/verify_elf.go
# спровоцировать ретрансмиты (например, tc netem loss на loopback) и убедиться,
# что события появляются в ring buffer (проверяется полноценно в TASK-05).
```

## Критерии приёмки (Definition of Done)

1. `bpf/tcploss.bpf.c` компилируется в `.o` через `bpf/Makefile` без ошибок и предупреждений верификатора.
2. Программа цепляется к `tracepoint/tcp/tcp_retransmit_skb`, фильтрует `AF_INET`.
3. Событие имеет ЗАФИКСИРОВАННЫЙ размер/офсеты, задокументированные в комментарии.
4. В hot-path НЕТ `bpf_printk`.
5. `.o` лежит в `pkg/embedded/bpf/tcploss.bpf.o`.
6. Загрузка программы проверена на реальном ядре (5.14+, с `/sys/kernel/btf/vmlinux`).

## Ограничения / риски

- IPv6 не поддерживается на этом этапе — явно задокументируй.
- Имя структуры контекста трейспоинта может отличаться между версиями `vmlinux.h`
  — сверяйся с фактическим `vmlinux.h` в репо и рантайм-форматом трейспоинта.
- Байт-порядок адресов/портов — единственный источник тонких багов; фиксируй соглашение и придерживайся его в TASK-05.
