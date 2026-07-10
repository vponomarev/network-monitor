# TASK-01 — Починить `bpf/vmlinux.h` (trace_entry) и загрузку tcploss на ядре 6.17

**Метка исполнителя:** 🧠 strong 🐧 linux-host (нужен доступ к ядру **6.17**)
**Зависит от:** —
**Блокирует:** TASK-02, TASK-11
**Оценка:** ~1 день

---

## Контекст (проблема)

`tcploss.bpf.o` **не грузится на ядре 6.17-azure** (раннер GitHub CI):
```
failed to resolve CO-RE relocation <byte_off> struct trace_event_raw_tcp_event_sk_skb.family (0:6 @ offset 48)
libbpf: prog 'handle_tcp_retransmit': failed to load: -22
```
На ядрах 5.15/6.1/6.8/6.12 та же программа грузится и работает (проверено в Фазе 1).

**Корень проблемы** (подтверждено анализом BTF-арифметики):
`bpf/vmlinux.h` — **рукописный, урезанный** заголовок (не сгенерированный из BTF).
В нём неверно объявлен общий заголовок трейспоинта:

`bpf/vmlinux.h:280-287`:
```c
struct trace_entry {
    unsigned short type;
    unsigned char  flags;
    unsigned char  preempt_count;
    pid_t pid;                    // pid_t == __kernel_long_t == long == 8 байт!
    pid_t tid;                    // ЛИШНЕЕ поле — в ядре его НЕТ
};                                // sizeof = 24, а в ядре = 8
```
- `pid_t` определён как `long` (`bpf/vmlinux.h:17-18`) → `pid` занимает 8 байт вместо 4 (в ядре это `int`).
- Добавлено несуществующее поле `tid`.

Из-за этого `sizeof(struct trace_entry)` = **24** вместо **8**, и все поля обеих
tracepoint-структур сдвинуты на +16 байт. В `tcploss` **типы полей верны**
(`__u16 sport/dport/family`, `__u8 saddr[4]`), поэтому CO-RE релоцирует смещение
(наш локальный 48 → реальный 32) и на 5.15–6.12 всё работает. На 6.17, судя по
всему, BTF-описание `trace_event_raw_tcp_event_sk_skb` изменилось (поле
отсутствует/переименовано/другой тип), и **принудительная CO-RE-релокация**
(`BPF_CORE_READ`) превращается в жёсткую ошибку загрузки вместо мягкой деградации.

**Как tcploss читает контекст** (`bpf/tcploss.bpf.c`):
- `SEC("tracepoint/tcp/tcp_retransmit_skb")`, `struct trace_event_raw_tcp_event_sk_skb *ctx` — строки 60-61
- `__u16 family = BPF_CORE_READ(ctx, family);` — строка 64 ← падающая релокация
- `bpf_core_read(&saddr, sizeof(saddr), &ctx->saddr);` — строка 79
- `bpf_core_read(&daddr, sizeof(daddr), &ctx->daddr);` — строка 80
- `evt->src_port = BPF_CORE_READ(ctx, sport);` — строка 98
- `evt->dst_port = BPF_CORE_READ(ctx, dport);` — строка 99

## Идея решения

Для **трейспоинтов** раскладка записи — стабильный ABI (файл
`/sys/kernel/tracing/events/<grp>/<name>/format`). Если структура ctx точно
совпадает с этим форматом, поля можно читать **напрямую** (`ctx->family`), и
компилятор **не генерирует `<byte_off>` CO-RE-релокаций** — libbpf нечего
«не суметь зарезолвить». Это строго устойчивее к расхождениям BTF, чем
принудительный CO-RE на рукописной структуре.

Поэтому:
1. Починить `struct trace_entry` в `bpf/vmlinux.h` (8 байт, без `tid`).
2. Убедиться, что `struct trace_event_raw_tcp_event_sk_skb` совпадает с реальным
   форматом трейспоинта (см. ниже).
3. Перевести чтение полей в `tcploss.bpf.c` на **прямой доступ** (`ctx->field`)
   вместо `BPF_CORE_READ`/`bpf_core_read`.

> Альтернатива — генерировать полный `vmlinux.h` из BTF (`bpftool btf dump`,
> цель уже есть в `bpf/Makefile:41-49`, но не подключена к сборке). Это тоже
> валидно, но делает `.o` зависимым от ядра сборки и утяжеляет заголовок на 100k+
> строк. **Рекомендуемый путь — прямое чтение + корректная минимальная структура.**

## Что сделать

1. **Исправить `struct trace_entry`** (`bpf/vmlinux.h:280-287`) — привести к ядру:
   ```c
   struct trace_entry {
       unsigned short type;
       unsigned char  flags;
       unsigned char  preempt_count;
       int            pid;      // int, 4 байта — НЕ pid_t/long; без поля tid
   };                            // sizeof == 8
   ```
   Проверь, что `pid_t` больше нигде не полагается на «длинный» размер; при
   необходимости оставь `typedef` как есть, но в `trace_entry` используй `int`.

2. **Выверить `struct trace_event_raw_tcp_event_sk_skb`** (`bpf/vmlinux.h:307-320`)
   по реальному формату (шаблон `tcp_event_sk_skb`, `include/trace/events/tcp.h`):
   ```c
   struct trace_event_raw_tcp_event_sk_skb {
       struct trace_entry ent;   // 0   (8)
       const void *skbaddr;      // 8
       const void *skaddr;       // 16
       int         state;        // 24
       __u16       sport;        // 28
       __u16       dport;        // 30
       __u16       family;       // 32
       __u8        saddr[4];     // 34
       __u8        daddr[4];     // 38
       __u8        saddr_v6[16]; // 42
       __u8        daddr_v6[16]; // 58
       char        __data[0];
   };
   ```
   (Типы уже верные — важно, что базовое смещение станет корректным после фикса
   `trace_entry`. Сверься с `format`-файлом на целевом хосте, см. «Как проверить».)

3. **Перевести чтение на прямой доступ** в `bpf/tcploss.bpf.c` (строки 64, 79-80,
   98-99): заменить `BPF_CORE_READ(ctx, X)` → `ctx->X` и
   `bpf_core_read(&dst, sizeof(dst), &ctx->saddr)` → `__builtin_memcpy(&dst, ctx->saddr, sizeof(dst))`
   (или почленно), т.к. структура теперь точно соответствует ABI. Убедись, что
   verifier доволен (границы). Если где-то остаётся чтение через `struct sock`
   (не tracepoint ctx) — его не трогать, это отдельный механизм.

4. **Пересобрать** и обновить embedded-объект:
   ```bash
   make -C bpf all
   cp bpf/tcploss.bpf.o pkg/embedded/bpf/
   ```

5. **Не сломать парсинг в Go.** Размер и раскладка Go-структуры `bpfLossEvent`
   (`internal/losscollector/ebpf_linux.go`) описывают **выходное** событие (48 байт),
   а не tracepoint-контекст, поэтому меняться не должны. Прогони
   `go test -run 'Validate|HandleRecord' ./internal/losscollector/...`.

## Критерии приёмки (Definition of Done)

1. `sizeof(struct trace_entry)` в `bpf/vmlinux.h` = 8; поля `tid` нет.
2. `tcploss.bpf.c` не использует `BPF_CORE_READ`/`bpf_core_read` для чтения полей
   tracepoint-контекста (прямой доступ).
3. `bpftool prog loadall bpf/tcploss.bpf.o` **успешно грузится** на ядрах
   **5.15, 6.1, 6.8, 6.12 и 6.17** (последнее — на VM владельца).
4. E2E на одном из ядер (желательно 6.17): при реальных ретрансмитах растёт
   `netmon_tcp_loss_total` с разметкой по ролям; `read == parsed`, `parse_errors=0`.
5. `pkg/embedded/bpf/tcploss.bpf.o` пересобран и закоммичен.
6. `go build ./... && go test ./... && gofmt -l .` — чисто.

## Как проверить
```bash
# 0. Реальный формат трейспоинта на целевом хосте (сверить поля/типы/смещения):
sudo cat /sys/kernel/tracing/events/tcp/tcp_retransmit_skb/format

# 1. Сборка и загрузка (на 6.17 VM и на прочих ядрах):
make -C bpf all
sudo bpftool prog loadall bpf/tcploss.bpf.o /sys/fs/bpf/_t
sudo bpftool prog show | grep tracepoint
sudo rm -rf /sys/fs/bpf/_t

# 2. E2E (лаборатория): сгенерировать потери и увидеть метрику
sudo tc qdisc add dev <iface> root netem loss 20%   # снять: tc qdisc del dev <iface> root
curl -s localhost:9876/metrics | grep -E 'netmon_tcp_loss_total|netmon_loss_events_(read|parsed)_total'
```

## Риски
- Прямое чтение полей требует, чтобы структура **точно** совпадала с ABI ядра.
  Обязательно сверься с `format`-файлом на 6.17 (поля могли добавить/переставить).
- Если на 6.17 формат `tcp_retransmit_skb` реально изменился — задокументировать
  в APPENDIX и, при необходимости, читать через `bpf_probe_read_kernel` от
  `struct sock` (fallback). Сначала попробовать прямой ABI-доступ.
- Не регенерируй `vmlinux.h` bare-командой `make` (перезапишет заголовок из BTF
  хоста сборки). Используй `make -C bpf all`.
