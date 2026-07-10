# TASK-04 — (C-2) Устранить busy-loop в ringbuf-ридере conntrack

**Метка исполнителя:** 👷 qwen-ok (🐧 linux-host для проверки; файл под build tag `linux`)
**Зависит от:** —
**Оценка:** ~0.5 дня

---

## Контекст (проблема)

`internal/conntrack/tracker_linux.go:377-422` (`readEvents`) при ошибке `rd.Read()`
делает `continue` только с Debug-логом (строки ~409-413). При закрытом reader'е
или устойчивой ошибке это **CPU-спин на 100%**. Нет обработки `ringbuf.ErrClosed`,
нет backoff.

В netmon аналогичная проблема уже решена в Фазе 1 (TASK-05,
`internal/losscollector/ebpf_linux.go`) — используй тот же образец.

## Эталон (как сделано в netmon)

`internal/losscollector/ebpf_linux.go`:
- На отмене контекста закрывается reader → блокирующий `Read()` возвращает
  `ringbuf.ErrClosed`.
- В цикле: `if errors.Is(err, ringbuf.ErrClosed) { return nil }` — чистый выход.
- На прочих ошибках — инкремент счётчика ошибок + короткий backoff/лог, без
  плотного спина.

## Что сделать

1. В `readEvents` (`tracker_linux.go`) обрабатывать `errors.Is(err, ringbuf.ErrClosed)`
   → корректно завершить горутину (не спинить).
2. При закрытии/остановке (`close`, отмена ctx) — закрывать `ringbuf.Reader`, чтобы
   `Read()` разблокировался (см. как netmon закрывает reader по `ctx.Done()`).
3. На прочих (не ErrClosed) ошибках — не делать голый `continue`: добавить учёт
   ошибки (счётчик, см. TASK-06) и небольшой backoff (например,
   `time.Sleep(10 * time.Millisecond)` или экспоненциальный), чтобы не спинить.
4. Импортировать `errors` и `github.com/cilium/ebpf/ringbuf`, если ещё не.

## Критерии приёмки (DoD)
1. При отмене контекста / закрытии reader горутина `readEvents` завершается без
   спина (проверяется тестом на context-cancel — он уже есть:
   `TestTracker_Run_ContextCancellation`).
2. Устойчивая ошибка чтения не приводит к 100% CPU (есть backoff).
3. `go build ./... && go test ./... && gofmt -l .` — чисто (тесты conntrack идут
   под Linux; проверять на Linux-хосте).

## Как проверить
```bash
# на Linux-хосте:
go test -run 'Tracker_Run_ContextCancellation|readEvents' ./internal/conntrack/...
# нагрузочно (опц.): запустить, снять eBPF/закрыть reader, убедиться в отсутствии спина (top)
```

## Риски
- Не менять семантику доставки событий в канал (`onConnectionEvent`), только
  цикл чтения/ошибок.
- Файл `tracker_linux.go` — Linux-only; на macOS не компилируется (заглушка
  `tracker_other.go`). Проверяй сборку с `GOOS=linux`.
