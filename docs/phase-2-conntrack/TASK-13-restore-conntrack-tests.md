# TASK-13 — Восстановить/переписать unit-тесты conntrack (после C-8)

**Метка исполнителя:** 👷 qwen-ok (🧠 strong для тестов конкурентности)
**Зависит от:** TASK-04, TASK-05, TASK-08 (тестируемое поведение стабилизировано)
**Оценка:** ~0.5–1 день

---

## Контекст (зачем)

В Фазе 1 (C-8) из `internal/conntrack/conntrack_linux_test.go` были **удалены**
тесты `TestTracker_sendEvent` и `TestTracker_simulateEvents` — они обращались к
уже удалённому API (`Tracker.sendEvent`, `simulateEvents(ctx)`, канал типа
`events.Event`). Комментарий в тесте (строки ~99-104) явно откладывает переписывание
на «conntrack track» — то есть на эту фазу.

Сейчас канал событий — `chan *Connection`; симуляция — `simulateEvents()`
(запускается по недоступности eBPF, а после TASK-08 — только по явному флагу).

## Что сделать

1. Переписать/добавить тесты под текущий API:
   - доставка события в канал `chan *Connection` (замена старого `sendEvent`);
   - симуляция: тест, что `simulateEvents` включается **только** по флагу (TASK-08),
     и что в проде без флага её нет;
   - ringbuf busy-loop / ErrClosed / backoff (TASK-04) — тест на корректное
     завершение и отсутствие спина (хотя бы на context-cancel);
   - PID→comm кэш (TASK-05): попадание/промах/TTL/дедуп; прогон под `-race`;
   - self-метрики (TASK-06): инкременты read/parsed/errors, `up` 1/0.
2. Сохранить существующие рабочие тесты (struct alignment, makeConnectionKey,
   state machine, sanitize/comm).
3. Тесты под build tag `linux` там, где касаются `tracker_linux.go`.

## Критерии приёмки (DoD)
1. Нет ссылок на удалённый API; пакет `internal/conntrack` компилируется и
   тестируется на Linux.
2. Новые тесты покрывают: доставку в канал, флаг симуляции, завершение ридера,
   PID→comm кэш, self-метрики.
3. `go test -race ./internal/conntrack/...` зелёный на Linux.
4. `go build ./... && go test ./... && gofmt -l .` — чисто.

## Как проверить
```bash
GOOS=linux go build ./internal/conntrack/...
go test -race ./internal/conntrack/...
```

## Риски
- Тесты симуляции/таймеров могут быть флаки — использовать детерминированные
  таймауты/инъекцию времени, а не «спать и надеяться».
- Не тестировать реальную загрузку eBPF в unit-тестах (это для smoke-load в CI,
  TASK-11) — здесь только Go-логика.
