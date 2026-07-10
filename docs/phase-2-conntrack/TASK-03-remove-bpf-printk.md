# TASK-03 — (C-1) Убрать `bpf_printk` из hot-path conntrack

**Метка исполнителя:** 👷 qwen-ok 🐧 linux-host (для пересборки/проверки)
**Зависит от:** —
**Оценка:** ~0.5 дня

---

## Контекст (проблема)

`bpf/conntrack.bpf.c` вызывает `bpf_printk` на **каждое исходящее соединение**
(строки ~231, 258, 287, 307). Это:
- оверхед в ядре на hot-path;
- пишет в глобальный `/sys/kernel/tracing/trace_pipe` — засоряет его и мешает
  наблюдаемости (а раньше мешало и netmon в режиме `loss_source: tracepipe`).

В проде `bpf_printk` не место.

## Что сделать

1. Ввести флаг отладки в eBPF-программе:
   ```c
   const volatile bool debug = false;   // задаётся из userspace при желании
   ```
2. Обернуть каждый `bpf_printk(...)` (строки ~231, 258, 287, 307 — проверь
   актуальные после TASK-02) в `if (debug) { bpf_printk(...); }`, либо удалить
   вовсе, если отладочный вывод не нужен. Предпочтительно — оставить под `debug`,
   чтобы можно было включить при диагностике.
3. Убедиться, что при `debug=false` (дефолт) в `trace_pipe` ничего не пишется.
4. Пересобрать и обновить embedded:
   ```bash
   make -C bpf all
   cp bpf/conntrack.bpf.o pkg/embedded/bpf/
   ```

> Если оставляешь `const volatile bool debug` — это read-only переменная в
> `.rodata`, её можно (опционально, отдельной задачей) прокидывать из Go через
> `spec.RewriteConstants`/`Variables`. В рамках этой задачи достаточно дефолта `false`.

## Критерии приёмки (DoD)
1. При дефолтной сборке `bpf_printk` не вызывается на hot-path (нет записей в
   `trace_pipe` при активных соединениях).
2. Программа грузится и работает (соединения по-прежнему трекаются).
3. `pkg/embedded/bpf/conntrack.bpf.o` пересобран и закоммичен.
4. `go build ./... && go test ./... && gofmt -l .` — чисто.

## Как проверить
```bash
make -C bpf all
sudo bpftool prog loadall bpf/conntrack.bpf.o /sys/fs/bpf/_c && sudo rm -rf /sys/fs/bpf/_c
# запустить conntrack, создать исходящие соединения и убедиться, что trace_pipe тихий:
sudo timeout 5 cat /sys/kernel/tracing/trace_pipe   # не должно быть строк от conntrack
```

## Риски
- Не удаляй заодно полезные счётчики/логику — только `bpf_printk`.
- После TASK-02 номера строк сместятся — ищи по подстроке `bpf_printk`.
