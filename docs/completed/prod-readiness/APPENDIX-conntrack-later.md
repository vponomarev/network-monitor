# APPENDIX — Отложенные задачи по conntrack (не для текущего этапа)

Эти проблемы найдены в коде conntrack (eBPF-трекинг соединений). Он остаётся в том
же бинаре netmon, но НЕ в фокусе текущего плана (нужен позже). Фиксируем здесь,
чтобы не потерять. **Не выполнять без отдельного запроса владельца.**

---

## C-1. Убрать `bpf_printk` из hot-path conntrack
`bpf/conntrack.bpf.c`, строки ~231, 258, 287, 307. `bpf_printk` вызывается на
**каждое исходящее соединение**. Это (а) оверхед в ядре, (б) пишет в `trace_pipe`.
Пока netmon использует `loss_source: tracepipe`, это ещё и засоряет источник данных
netmon. После перехода netmon на eBPF-loss (TASK-04..06) связь ослабевает, но printk
в проде всё равно не место. Убрать или закрыть под `volatile const bool debug`.

## C-2. Busy-loop при ошибке чтения ringbuf
`internal/conntrack/tracker_linux.go:409-413` — при ошибке `rd.Read()` делается
`continue` только с Debug-логом. При закрытом reader'е / устойчивой ошибке это
CPU-спин на 100%. Исправить по образцу TASK-05 (обработка `ringbuf.ErrClosed` +
backoff). Тот же паттерн нужно применить и здесь.

## C-3. Синхронный `/proc/{pid}/comm` в пути обработки событий
`internal/conntrack/tracker_linux.go:461, 526` (`enrichProcessName` → `getProcessComm`)
делает синхронный read из `/proc` на каждом событии в потребителе ringbuf. При
высоком churn соединений добавляет задержку и провоцирует дропы. Вынести в
асинхронное обогащение / кэш PID→comm с TTL.

## C-4. Дропы событий conntrack без явной метрики в /metrics
`droppedEvents` считается (`atomic`), но убедись, что он экспонируется как
Prometheus-метрика (в `internal/conntrack/metrics.go` есть `UpdateDroppedMetrics` —
проверить, что она реально регистрируется и видна в `/metrics`).

## C-5. IPv6 для conntrack
Сейчас только `AF_INET`. Если понадобится IPv6 — отдельная задача (расширение
eBPF-программы и парсинга).

## C-6. Симуляция событий в проде
`simulateEvents` (`tracker_linux.go:621`) включается, если eBPF недоступен. В проде
это может незаметно подменить реальные данные фейковыми. Рассмотреть: в прод-режиме
при недоступности eBPF — ошибка/`up=0`, а не тихая симуляция.

## C-7. conntrack.bpf.o не грузится на новых ядрах (CO-RE relocation)
Обнаружено при работе над TASK-12 (smoke-load на ядре 6.12). `bpftool prog loadall
bpf/conntrack.bpf.o` падает:
```
<invalid CO-RE relocation>
failed to resolve CO-RE relocation <byte_off> [48] struct trace_event_raw_inet_sock_set_state.saddr
libbpf: prog 'trace_outgoing': failed to load: -22
```
Причина: ручная структура `trace_event_raw_inet_sock_set_state` в `bpf/vmlinux.h`
объявляет все поля как `__u32` (oldstate/newstate/sport/dport/family/protocol/saddr/…),
а реальная BTF ядра имеет другие типы/офсеты (`__u16 sport/dport/family/protocol`,
`__u8 saddr[4]`), поэтому CO-RE не может релоцировать офсет `saddr`. На старых ядрах
проходило, на 6.8/6.12 — нет.
**Как чинить:** объявить структуру с корректными типами (как сделано для
`trace_event_raw_tcp_event_sk_skb` в TASK-04) ИЛИ генерировать полный `vmlinux.h`
через `bpftool btf dump`. После фикса — smoke-load conntrack в `ebpf-build.yml`
сделать блокирующим (сейчас `continue-on-error`).

## C-8. (ИСПРАВЛЕНО в TASK-12) устаревший тест conntrack
`conntrack_linux_test.go::TestTracker_connectionKey` вызывал несуществующий
`tracker.connectionKey` → тест-пакет conntrack не компилировался на Linux (на macOS
скрыто build-тегом), из-за чего job `test` в `ci.yml` был красным. Исправлено:
вызов заменён на свободную функцию `makeConnectionKey(...)`.

---

> Приоритет при возврате к conntrack: **C-7** (переносимость eBPF — блокер для
> conntrack на новых ядрах), C-2 и C-1 (стабильность/производительность),
> затем C-4 (наблюдаемость), затем C-3, C-6, C-5.
